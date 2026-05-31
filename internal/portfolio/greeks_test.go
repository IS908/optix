package portfolio

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// fakePricer implements OptionPricer deterministically for unit tests.
type fakePricer struct {
	// greeks keyed by strike for predictability; default returned otherwise.
	perShare model.Greeks
	ivOK     bool
	iv       float64
}

func (f *fakePricer) PriceOption(_ context.Context, _, _, _, _, _ float64, _ model.OptionType) (model.Greeks, error) {
	return f.perShare, nil
}
func (f *fakePricer) ImpliedVol(_ context.Context, _, _, _, _, _ float64, _ model.OptionType) (float64, bool, error) {
	return f.iv, f.ivOK, nil
}

// fakeChains implements ChainProvider from an in-memory map keyed by symbol.
type fakeChains map[string]*model.OptionChain

func (f fakeChains) GetOptionChain(_ context.Context, underlying, _ string) (*model.OptionChain, error) {
	return f[underlying], nil // nil chain → aggregator falls back to mark IV
}

func TestRightToOptionType(t *testing.T) {
	if rightToOptionType("C") != model.OptionTypeCall {
		t.Errorf("C should map to Call")
	}
	if rightToOptionType("P") != model.OptionTypePut {
		t.Errorf("P should map to Put")
	}
}

var _ = math.Abs // keep import for later tests in this file

func mkChain(underlying string, spot float64, q model.OptionQuote) *model.OptionChain {
	exp := model.OptionChainExpiry{Expiration: "2026-06-19", DaysToExpiry: 19}
	if q.OptionType == model.OptionTypePut {
		exp.Puts = []model.OptionQuote{q}
	} else {
		exp.Calls = []model.OptionQuote{q}
	}
	return &model.OptionChain{Underlying: underlying, UnderlyingPrice: spot, Expirations: []model.OptionChainExpiry{exp}}
}

func TestResolveIV_FromChain(t *testing.T) {
	chain := mkChain("GOOGL", 180, model.OptionQuote{
		Strike: 185, OptionType: model.OptionTypeCall, ImpliedVolatility: 0.31,
	})
	iv, src, ok := resolveIV(context.Background(),
		heldLeg{Strike: 185, OptionType: model.OptionTypeCall, Mark: 4.0, TYears: 0.05},
		chain, 180, 0.043, &fakePricer{})
	if !ok || src != "chain" || math.Abs(iv-0.31) > 1e-9 {
		t.Fatalf("got iv=%v src=%q ok=%v, want 0.31/chain/true", iv, src, ok)
	}
}

func TestResolveIV_FallsBackToMark(t *testing.T) {
	// Chain present but no matching strike → mark inversion.
	chain := mkChain("GOOGL", 180, model.OptionQuote{
		Strike: 999, OptionType: model.OptionTypeCall, ImpliedVolatility: 0.31,
	})
	iv, src, ok := resolveIV(context.Background(),
		heldLeg{Strike: 185, OptionType: model.OptionTypeCall, Mark: 4.0, TYears: 0.05},
		chain, 180, 0.043, &fakePricer{iv: 0.27, ivOK: true})
	if !ok || src != "mark" || math.Abs(iv-0.27) > 1e-9 {
		t.Fatalf("got iv=%v src=%q ok=%v, want 0.27/mark/true", iv, src, ok)
	}
}

func TestResolveIV_SkipWhenNoIVAndNoMark(t *testing.T) {
	_, _, ok := resolveIV(context.Background(),
		heldLeg{Strike: 185, OptionType: model.OptionTypeCall, Mark: 0, TYears: 0.05},
		nil, 180, 0.043, &fakePricer{ivOK: false})
	if ok {
		t.Fatalf("expected skip (ok=false) when chain nil and mark 0")
	}
}

func TestResolveIV_SkipNearExpiry(t *testing.T) {
	chain := mkChain("GOOGL", 180, model.OptionQuote{
		Strike: 185, OptionType: model.OptionTypeCall, ImpliedVolatility: 0.31,
	})
	_, _, ok := resolveIV(context.Background(),
		heldLeg{Strike: 185, OptionType: model.OptionTypeCall, Mark: 4.0, TYears: 0.001},
		chain, 180, 0.043, &fakePricer{})
	if ok {
		t.Fatalf("expected skip for T <= 1 day even with chain IV")
	}
}

func optionPos(sym, right string, qty, strike float64, mark float64) model.Position {
	return model.Position{
		Symbol: sym, SecType: "OPT", Expiration: "20260619", Strike: strike,
		Right: right, Quantity: qty, Multiplier: 100, LastPrice: mark,
		MarketValue: qty * mark * 100, Currency: "USD",
	}
}

func greeksOpts(by string) GreeksOptions {
	return GreeksOptions{GroupBy: by, NetLiqUSD: 1_000_000, RiskFreeRate: 0.043, AsOf: refTime()}
}

func refTime() time.Time { return time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC) }

func TestAggregate_StockLegDeltaIsShares(t *testing.T) {
	pos := []model.Position{stockPos("MSFT", 100, 450)}
	r, err := AggregateGreeks(context.Background(), pos, greeksOpts("underlying"),
		&fakePricer{}, fakeChains{}, fakeSectorMap())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 1 || r.Groups[0].Key != "MSFT" {
		t.Fatalf("groups = %+v", r.Groups)
	}
	if r.Groups[0].NetDelta != 100 || r.Groups[0].Gamma != 0 || r.Groups[0].Theta != 0 {
		t.Errorf("stock leg: NetDelta=%v Gamma=%v Theta=%v, want 100/0/0",
			r.Groups[0].NetDelta, r.Groups[0].Gamma, r.Groups[0].Theta)
	}
	// DollarDelta = MarketValue * 0.01 = (100*450) * 0.01 = 450 (USD per +1% spot)
	if math.Abs(r.Groups[0].DollarDelta-450) > 1e-6 {
		t.Errorf("stock DollarDelta = %v, want 450 (1%% of $45,000 MV)", r.Groups[0].DollarDelta)
	}
}

func TestAggregate_OptionLegFromChainIV(t *testing.T) {
	pos := []model.Position{optionPos("GOOGL", "C", 2, 185, 4.0)}
	chain := mkChain("GOOGL", 180, model.OptionQuote{
		Strike: 185, OptionType: model.OptionTypeCall, ImpliedVolatility: 0.31,
	})
	pricer := &fakePricer{perShare: model.Greeks{Delta: 0.5, Gamma: 0.01, Vega: 0.2, Theta: -0.05}}
	r, err := AggregateGreeks(context.Background(), pos, greeksOpts("underlying"),
		pricer, fakeChains{"GOOGL": chain}, fakeSectorMap())
	if err != nil {
		t.Fatal(err)
	}
	g := r.Groups[0]
	// NetDelta = 0.5 * 2 * 100 = 100
	if math.Abs(g.NetDelta-100) > 1e-9 {
		t.Errorf("NetDelta = %v, want 100", g.NetDelta)
	}
	// DollarDelta = NetDelta * spot * 0.01 = 100 * 180 * 0.01 = 180 (USD per +1% spot)
	if math.Abs(g.DollarDelta-180) > 1e-6 {
		t.Errorf("DollarDelta = %v, want 180 (USD per +1%% spot)", g.DollarDelta)
	}
	// Gamma display = (0.01*2*100) * spot * 0.01 = 2 * 180 * 0.01 = 3.6
	if math.Abs(g.Gamma-3.6) > 1e-6 {
		t.Errorf("Gamma = %v, want 3.6", g.Gamma)
	}
	// Vega display = (0.2*2*100) / 100 = 0.4
	if math.Abs(g.Vega-0.4) > 1e-6 {
		t.Errorf("Vega = %v, want 0.4", g.Vega)
	}
	// Theta display = -0.05*2*100 = -10
	if math.Abs(g.Theta-(-10)) > 1e-6 {
		t.Errorf("Theta = %v, want -10", g.Theta)
	}
	if g.IVSource != "chain" {
		t.Errorf("IVSource = %q, want chain", g.IVSource)
	}
}

func TestAggregate_SkipWhenNoIV(t *testing.T) {
	pos := []model.Position{optionPos("RKLB", "C", 1, 16, 0)} // mark 0, no chain
	r, err := AggregateGreeks(context.Background(), pos, greeksOpts("underlying"),
		&fakePricer{ivOK: false}, fakeChains{}, fakeSectorMap())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.SkippedLegs) != 1 || r.SkippedLegs[0].Symbol != "RKLB" {
		t.Fatalf("SkippedLegs = %+v, want 1 RKLB", r.SkippedLegs)
	}
	if r.Total.NetDelta != 0 {
		t.Errorf("skipped leg must not contribute, NetDelta=%v", r.Total.NetDelta)
	}
}

func TestAggregate_NonUSDAndResidualExcluded(t *testing.T) {
	pos := []model.Position{
		stockPos("MSFT", 100, 450),
		{Symbol: "0700", SecType: "STK", Quantity: 100, MarketValue: 38000, Multiplier: 1, Currency: "HKD", LastPrice: 380},
		{Symbol: "DEAD", SecType: "OPT", Quantity: 0, Multiplier: 100, LastPrice: 1, Currency: "USD"},
	}
	r, _ := AggregateGreeks(context.Background(), pos, greeksOpts("underlying"),
		&fakePricer{}, fakeChains{}, fakeSectorMap())
	for _, g := range r.Groups {
		if g.Key == "0700" || g.Key == "DEAD" {
			t.Errorf("excluded ticker present: %s", g.Key)
		}
	}
}

func TestAggregate_ShortPositionSigns(t *testing.T) {
	// Short 5 puts: signedQty negative; theta should flip sign vs long.
	pos := []model.Position{optionPos("TSLA", "P", -5, 200, 3.0)}
	chain := mkChain("TSLA", 210, model.OptionQuote{
		Strike: 200, OptionType: model.OptionTypePut, ImpliedVolatility: 0.5,
	})
	pricer := &fakePricer{perShare: model.Greeks{Delta: -0.3, Gamma: 0.02, Vega: 0.4, Theta: -0.10}}
	r, _ := AggregateGreeks(context.Background(), pos, greeksOpts("underlying"),
		pricer, fakeChains{"TSLA": chain}, fakeSectorMap())
	g := r.Groups[0]
	// NetDelta = -0.3 * -5 * 100 = +150
	if math.Abs(g.NetDelta-150) > 1e-9 {
		t.Errorf("NetDelta = %v, want +150 (short put = positive delta)", g.NetDelta)
	}
	// Theta = -0.10 * -5 * 100 = +50 (short earns time decay)
	if math.Abs(g.Theta-50) > 1e-9 {
		t.Errorf("Theta = %v, want +50 (short earns theta)", g.Theta)
	}
}

func TestAggregate_BySector(t *testing.T) {
	pos := []model.Position{stockPos("GOOGL", 100, 400), stockPos("MSFT", 100, 450)}
	r, _ := AggregateGreeks(context.Background(), pos, greeksOpts("sector"),
		&fakePricer{}, fakeChains{}, fakeSectorMap())
	// fakeSectorMap maps both GOOGL and MSFT to "mega-cap-tech".
	if len(r.Groups) != 1 || r.Groups[0].Key != "mega-cap-tech" {
		t.Fatalf("groups = %+v, want one mega-cap-tech", r.Groups)
	}
	if math.Abs(r.Groups[0].NetDelta-200) > 1e-9 {
		t.Errorf("NetDelta = %v, want 200", r.Groups[0].NetDelta)
	}
}

func TestAggregate_NonFiniteGreeksSkipped(t *testing.T) {
	pos := []model.Position{optionPos("GOOGL", "C", 1, 185, 4.0)}
	chain := mkChain("GOOGL", 180, model.OptionQuote{
		Strike: 185, OptionType: model.OptionTypeCall, ImpliedVolatility: 0.31,
	})
	pricer := &fakePricer{perShare: model.Greeks{Delta: math.NaN()}}
	r, _ := AggregateGreeks(context.Background(), pos, greeksOpts("underlying"),
		pricer, fakeChains{"GOOGL": chain}, fakeSectorMap())
	if len(r.SkippedLegs) != 1 || r.SkippedLegs[0].Reason != "non_finite" {
		t.Fatalf("SkippedLegs = %+v, want 1 non_finite", r.SkippedLegs)
	}
}
