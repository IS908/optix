package portfolio

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func TestRunStressUsesDollarDeltaDollarGammaAndIVPointShocks(t *testing.T) {
	report := &GreeksReport{
		SnapshotAt: time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC),
		NetLiqUSD:  100_000,
		Groups: []GreeksGroup{
			{Key: "AAPL", DollarDelta: 1000, DollarGamma: 25, Vega: -20},
			{Key: "TSLA", DollarDelta: -500, DollarGamma: 5, Vega: -100},
		},
	}
	scenarios := []StressScenario{{
		ID:    "spy-down-iv-up",
		Label: "SPY down, IV up",
		Shocks: []StressShock{
			{Axis: "spy_pct", Magnitude: -0.03},
			{Axis: "iv_points", Magnitude: 5},
		},
	}}

	out := RunStress(report, scenarios)
	if len(out.Scenarios) != 1 {
		t.Fatalf("scenarios = %+v", out.Scenarios)
	}
	got := out.Scenarios[0]
	// Linearized P&L per group:
	// AAPL (beta 1.2): 1000*-3.6 + 0.5*25*12.96 + (-20*5) = -3538
	// TSLA (beta 1.6): -500*-4.8 + 0.5*5*23.04 + (-100*5) = 1957.6
	want := -1580.4
	if math.Abs(got.TotalPnLUSD-want) > 1e-9 {
		t.Fatalf("TotalPnLUSD = %v, want %v", got.TotalPnLUSD, want)
	}
	if math.Abs(got.PctNLV-(-1.5804)) > 1e-9 {
		t.Fatalf("PctNLV = %v, want -1.5804", got.PctNLV)
	}
	if got.WorstPosition.Key != "AAPL" || math.Abs(got.WorstPosition.PnLUSD-(-3538)) > 1e-9 {
		t.Fatalf("worst = %+v", got.WorstPosition)
	}
}

func TestRunStressScalesBroadIndexShockBySymbolBeta(t *testing.T) {
	report := &GreeksReport{
		NetLiqUSD: 100_000,
		Groups: []GreeksGroup{
			{Key: "TSLA", DollarDelta: 1000},
			{Key: "KO", DollarDelta: 1000},
		},
	}

	out := RunStress(report, []StressScenario{{
		ID: "spy-down-10", Label: "SPY -10%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.10}},
	}})

	got := pnlByKey(out.Scenarios[0].Positions)
	if math.Abs(got["TSLA"]-(-16000)) > 1e-9 {
		t.Fatalf("TSLA P&L = %v, want -16000 with beta 1.6", got["TSLA"])
	}
	if got["KO"] != -5000 {
		t.Fatalf("KO P&L = %v, want -5000 with beta 0.5", got["KO"])
	}
}

func TestRunStressPrefersHistoricalBetaProvider(t *testing.T) {
	report := &GreeksReport{
		NetLiqUSD: 100_000,
		Groups:    []GreeksGroup{{Key: "TSLA", DollarDelta: 1000}},
	}
	provider := StaticStressBetaProvider(map[string]StressBetaSource{
		"TSLA": {Key: "TSLA", Beta: 2.0, Source: "historical_cache", Observations: 60},
	})

	out := RunStressWithBetaProvider(report, []StressScenario{{
		ID: "spy-down-10", Label: "SPY -10%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.10}},
	}}, provider)

	got := pnlByKey(out.Scenarios[0].Positions)
	if got["TSLA"] != -20000 {
		t.Fatalf("TSLA P&L = %v, want -20000 with beta 2.0", got["TSLA"])
	}
	if len(out.BetaSources) != 1 || out.BetaSources[0].Source != "historical_cache" || out.BetaSources[0].Beta != 2.0 {
		t.Fatalf("BetaSources = %+v, want historical beta 2.0", out.BetaSources)
	}
}

func TestRunStressUsesNonPositiveHistoricalBetaProvider(t *testing.T) {
	report := &GreeksReport{
		NetLiqUSD: 100_000,
		Groups:    []GreeksGroup{{Key: "HEDGE", DollarDelta: 1000}},
	}
	provider := StaticStressBetaProvider(map[string]StressBetaSource{
		"HEDGE": {Key: "HEDGE", Beta: -0.5, Source: "historical_computed", Observations: 60},
	})

	out := RunStressWithBetaProvider(report, []StressScenario{{
		ID: "spy-down-10", Label: "SPY -10%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.10}},
	}}, provider)

	got := pnlByKey(out.Scenarios[0].Positions)
	if got["HEDGE"] != 5000 {
		t.Fatalf("HEDGE P&L = %v, want +5000 with beta -0.5", got["HEDGE"])
	}
	if len(out.BetaSources) != 1 || out.BetaSources[0].Beta != -0.5 {
		t.Fatalf("BetaSources = %+v, want historical beta -0.5", out.BetaSources)
	}
}

func TestRunStressWithRepricingUsesPerLegPriceBeforeTaylorFallback(t *testing.T) {
	report := &GreeksReport{
		NetLiqUSD: 100_000,
		Groups: []GreeksGroup{{
			Key: "AAPL", DollarDelta: 9999, DollarGamma: 9999, Vega: 9999,
		}},
		StressOptionLegs: []StressOptionLeg{{
			Key: "AAPL", ShockKey: "AAPL", Spot: 100, Strike: 100, TYears: 0.25,
			RiskFreeRate: 0.04, IV: 0.20, BasePrice: 10, SignedQty: 1, Multiplier: 100,
			OptionType: callOptionTypeForStressTest(),
		}},
	}
	pricer := &fakeStressRepricePricer{price: 15}
	provider := StaticStressBetaProvider{"AAPL": {Key: "AAPL", Beta: 1, Source: "historical_cache"}}

	out := RunStressWithRepricing(context.Background(), report, []StressScenario{{
		ID: "spy-down-iv-up", Label: "SPY down IV up", Shocks: []StressShock{
			{Axis: "spy_pct", Magnitude: -0.10},
			{Axis: "iv_points", Magnitude: 5},
		},
	}}, provider, pricer)

	if len(pricer.calls) != 1 {
		t.Fatalf("PriceOption calls = %d, want 1", len(pricer.calls))
	}
	call := pricer.calls[0]
	if math.Abs(call.spot-90) > 1e-9 || math.Abs(call.iv-0.25) > 1e-9 {
		t.Fatalf("repricer called with spot=%v iv=%v, want 90/0.25", call.spot, call.iv)
	}
	got := pnlByKey(out.Scenarios[0].Positions)
	if got["AAPL"] != 500 {
		t.Fatalf("AAPL P&L = %v, want (15 - 10) * 100 = 500", got["AAPL"])
	}
}

func TestRunStressWithRepricingFallsBackToTaylorWhenRepriceFails(t *testing.T) {
	report := &GreeksReport{
		NetLiqUSD: 100_000,
		Groups:    []GreeksGroup{{Key: "AAPL"}},
		StressOptionLegs: []StressOptionLeg{{
			Key: "AAPL", ShockKey: "AAPL", Spot: 100, Strike: 100, TYears: 0.25,
			RiskFreeRate: 0.04, IV: 0.20, BasePrice: 10, SignedQty: 1, Multiplier: 100,
			OptionType: model.OptionTypeCall, FallbackDollarDelta: 100, FallbackDollarGamma: 10, FallbackVega: 2,
		}},
	}
	pricer := &fakeStressRepricePricer{err: errors.New("repricer down")}

	out := RunStressWithRepricing(context.Background(), report, []StressScenario{{
		ID: "underlying-up-iv-up", Label: "Underlying up IV up", Shocks: []StressShock{
			{Axis: "underlying_pct", Magnitude: 0.01},
			{Axis: "iv_points", Magnitude: 2},
		},
	}}, nil, pricer)

	got := pnlByKey(out.Scenarios[0].Positions)
	want := 100*1 + 0.5*10*1 + 2*2
	if got["AAPL"] != want {
		t.Fatalf("AAPL P&L = %v, want Taylor fallback %v", got["AAPL"], want)
	}
}

func TestRunStressWithRepricingCircuitBreaksAfterFirstRepriceFailure(t *testing.T) {
	report := &GreeksReport{
		NetLiqUSD: 100_000,
		Groups:    []GreeksGroup{{Key: "AAPL"}},
		StressOptionLegs: []StressOptionLeg{
			{
				Key: "AAPL", ShockKey: "AAPL", Spot: 100, Strike: 100, TYears: 0.25,
				RiskFreeRate: 0.04, IV: 0.20, BasePrice: 10, SignedQty: 1, Multiplier: 100,
				OptionType: model.OptionTypeCall, FallbackDollarDelta: 100,
			},
			{
				Key: "AAPL", ShockKey: "AAPL", Spot: 100, Strike: 100, TYears: 0.25,
				RiskFreeRate: 0.04, IV: 0.20, BasePrice: 10, SignedQty: 1, Multiplier: 100,
				OptionType: model.OptionTypeCall, FallbackDollarDelta: 200,
			},
		},
	}
	pricer := &fakeStressRepricePricer{err: errors.New("repricer down")}

	out := RunStressWithRepricing(context.Background(), report, []StressScenario{{
		ID: "underlying-up", Label: "Underlying up", Shocks: []StressShock{{Axis: "underlying_pct", Magnitude: 0.01}},
	}}, nil, pricer)

	if len(pricer.calls) != 1 {
		t.Fatalf("PriceOption calls = %d, want 1 before Taylor circuit breaker", len(pricer.calls))
	}
	got := pnlByKey(out.Scenarios[0].Positions)
	if got["AAPL"] != 300 {
		t.Fatalf("AAPL P&L = %v, want both legs via Taylor fallback", got["AAPL"])
	}
}

func TestRunStressReportsStaticFallbackBetaSource(t *testing.T) {
	report := &GreeksReport{
		NetLiqUSD: 100_000,
		Groups:    []GreeksGroup{{Key: "KO", DollarDelta: 1000}},
	}

	out := RunStress(report, []StressScenario{{
		ID: "spy-down-10", Label: "SPY -10%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.10}},
	}})

	if len(out.BetaSources) != 1 || out.BetaSources[0].Source != "static_fallback" || out.BetaSources[0].Beta != 0.5 {
		t.Fatalf("BetaSources = %+v, want static KO beta 0.5", out.BetaSources)
	}
}

func TestRunStressFallbackBetaJSONOmitsFreshnessTimestamps(t *testing.T) {
	out := RunStress(&GreeksReport{
		NetLiqUSD: 100_000,
		Groups:    []GreeksGroup{{Key: "KO", DollarDelta: 1000}},
	}, []StressScenario{{
		ID: "spy-down-10", Label: "SPY -10%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.10}},
	}})

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), "as_of") || strings.Contains(string(data), "updated_at") {
		t.Fatalf("fallback beta JSON should omit freshness timestamps: %s", data)
	}
}

func TestRunStressQQQShockTargetsNasdaqNamesOnly(t *testing.T) {
	report := &GreeksReport{
		NetLiqUSD: 100_000,
		Groups: []GreeksGroup{
			{Key: "AAPL", DollarDelta: 1000},
			{Key: "XOM", DollarDelta: 1000},
		},
	}

	out := RunStress(report, []StressScenario{{
		ID: "qqq-down-5", Label: "QQQ -5%", Shocks: []StressShock{{Axis: "qqq_pct", Magnitude: -0.05}},
	}})

	got := pnlByKey(out.Scenarios[0].Positions)
	if got["AAPL"] != -6000 {
		t.Fatalf("AAPL P&L = %v, want -6000 with beta 1.2", got["AAPL"])
	}
	if got["XOM"] != 0 {
		t.Fatalf("XOM P&L = %v, want 0 because XOM is not a QQQ target", got["XOM"])
	}
}

func TestRunStressCarriesSkippedLegsFromGreeksSnapshot(t *testing.T) {
	report := &GreeksReport{
		NetLiqUSD: 100_000,
		Groups:    []GreeksGroup{{Key: "AAPL", DollarDelta: 100}},
		SkippedLegs: []SkippedLeg{{
			Symbol: "RKLB", Expiration: "20260619", Right: "C", Strike: 16, Reason: "no_iv",
		}},
	}

	out := RunStress(report, []StressScenario{{
		ID: "spy-down-3", Label: "SPY -3%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.03}},
	}})

	if out.SkippedLegCount != 1 {
		t.Fatalf("SkippedLegCount = %d, want 1", out.SkippedLegCount)
	}
	if len(out.SkippedLegs) != 1 || out.SkippedLegs[0].Symbol != "RKLB" || out.SkippedLegs[0].Reason != "no_iv" {
		t.Fatalf("SkippedLegs = %+v, want RKLB no_iv", out.SkippedLegs)
	}
}

func TestRenderStressIncludesWorstTail(t *testing.T) {
	report := RunStress(&GreeksReport{
		NetLiqUSD: 100_000,
		Groups:    []GreeksGroup{{Key: "LONG_DELTA", DollarDelta: 100}},
	}, []StressScenario{{
		ID: "spy-down-3", Label: "SPY -3%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.03}},
	}})
	var b strings.Builder
	RenderStress(report, &b)
	out := b.String()
	if !strings.Contains(out, "PORTFOLIO STRESS TEST") || !strings.Contains(out, "SPY -3%") || !strings.Contains(out, "Worst tail") {
		t.Fatalf("render output missing expected text:\n%s", out)
	}
}

func TestRenderStressDoesNotSayPositiveTailCostsNLV(t *testing.T) {
	report := RunStress(&GreeksReport{
		NetLiqUSD: 100_000,
		Groups:    []GreeksGroup{{Key: "LONG_VEGA", Vega: 100}},
	}, []StressScenario{{
		ID: "iv-up-5", Label: "IV +5 pts", Shocks: []StressShock{{Axis: "iv_points", Magnitude: 5}},
	}})
	var b strings.Builder
	RenderStress(report, &b)
	out := b.String()
	if strings.Contains(out, "costs") {
		t.Fatalf("positive stress scenario should not be rendered as a cost:\n%s", out)
	}
	if !strings.Contains(out, "Least favorable") || !strings.Contains(out, "gains 0.5%") {
		t.Fatalf("positive stress scenario missing least-favorable gain summary:\n%s", out)
	}
}

func TestRenderStressWarnsWhenLegsWereSkipped(t *testing.T) {
	report := RunStress(&GreeksReport{
		NetLiqUSD: 100_000,
		Groups:    []GreeksGroup{{Key: "AAPL", DollarDelta: 100}},
		SkippedLegs: []SkippedLeg{{
			Symbol: "RKLB", Expiration: "20260619", Right: "C", Strike: 16, Reason: "no_iv",
		}},
	}, []StressScenario{{
		ID: "spy-down-3", Label: "SPY -3%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.03}},
	}})

	var b strings.Builder
	RenderStress(report, &b)
	out := b.String()
	if !strings.Contains(out, "1 leg(s) excluded") ||
		!strings.Contains(out, "stress may understate risk") ||
		!strings.Contains(out, "RKLB 20260619 C16") {
		t.Fatalf("stress skipped-leg warning missing expected detail:\n%s", out)
	}
}

func pnlByKey(positions []StressPositionPnL) map[string]float64 {
	out := make(map[string]float64, len(positions))
	for _, p := range positions {
		out[p.Key] = p.PnLUSD
	}
	return out
}

func callOptionTypeForStressTest() model.OptionType {
	return model.OptionTypeCall
}

type fakeStressRepricePricer struct {
	price float64
	err   error
	calls []stressRepriceCall
}

type stressRepriceCall struct {
	spot float64
	iv   float64
}

func (f *fakeStressRepricePricer) PriceOption(_ context.Context, spot, _, _, _, iv float64, _ model.OptionType) (model.Greeks, error) {
	f.calls = append(f.calls, stressRepriceCall{spot: spot, iv: iv})
	if f.err != nil {
		return model.Greeks{}, f.err
	}
	return model.Greeks{Price: f.price}, nil
}

func (f *fakeStressRepricePricer) ImpliedVol(context.Context, float64, float64, float64, float64, float64, model.OptionType) (float64, bool, error) {
	return 0, false, nil
}
