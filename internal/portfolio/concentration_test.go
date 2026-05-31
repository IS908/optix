package portfolio

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/IS908/optix/pkg/model"
)

// fakeSectorMap covers tickers used across these tests so sector grouping is
// exercised end-to-end. Tickers not listed fall back to "unclassified".
func fakeSectorMap() *SectorMap {
	return &SectorMap{
		SectorLabels: map[string]string{
			"mega-cap-tech":     "Mega-cap Tech",
			"ai-auto-mobility":  "AI Auto",
			"aerospace-space":   "Aerospace",
			"etf-broad-index":   "Index ETF",
		},
		TickerSectors: map[string]string{
			"GOOGL": "mega-cap-tech",
			"MSFT":  "mega-cap-tech",
			"TSLA":  "ai-auto-mobility",
			"RKLB":  "aerospace-space",
			"QQQ":   "etf-broad-index",
		},
	}
}

func stockPos(sym string, qty, mark float64) model.Position {
	return model.Position{
		Symbol:      sym,
		SecType:     "STK",
		Quantity:    qty,
		LastPrice:   mark,
		MarketValue: qty * mark,
		Multiplier:  1,
	}
}

// optPos builds an option position with a pre-computed MV. mvOverride lets
// tests inject a 0 MV to simulate a missing OPRA mark, which is exactly the
// pathology we want concentration to flag rather than silently under-weight.
func optPos(sym, right string, qty, strike, mark float64, mvOverride *float64) model.Position {
	p := model.Position{
		Symbol:      sym,
		SecType:     "OPT",
		Quantity:    qty,
		Strike:      strike,
		Right:       right,
		LastPrice:   mark,
		MarketValue: qty * mark * 100,
		Multiplier:  100,
	}
	if mvOverride != nil {
		p.MarketValue = *mvOverride
	}
	return p
}

func TestCompute_EmptyPositions(t *testing.T) {
	r := Compute(nil, 100_000, DefaultConfig(), fakeSectorMap())
	if len(r.Underlyings) != 0 {
		t.Fatalf("expected 0 underlyings, got %d", len(r.Underlyings))
	}
	if r.HHI != 0 {
		t.Fatalf("HHI should be 0 for empty portfolio, got %f", r.HHI)
	}
	if r.DeployedPct != 0 {
		t.Fatalf("DeployedPct should be 0, got %f", r.DeployedPct)
	}
	if len(r.Flags) != 0 {
		t.Fatalf("no flags expected, got %d", len(r.Flags))
	}
}

func TestCompute_SingleStockUnderRedNoWarn(t *testing.T) {
	pos := []model.Position{stockPos("MSFT", 100, 450)} // $45,000 MV
	r := Compute(pos, 500_000, DefaultConfig(), fakeSectorMap())
	if got := r.Underlyings[0].WeightPct; math.Abs(got-9.0) > 0.01 {
		t.Fatalf("expected weight 9.0%%, got %.2f", got)
	}
	if len(r.Flags) != 0 {
		t.Fatalf("9%% is under the 10%% warn threshold; expected 0 flags, got %d (%+v)", len(r.Flags), r.Flags)
	}
}

func TestCompute_StockAndOptionAggregateByUnderlying(t *testing.T) {
	// 100 shares TSLA at $400 = $40,000; plus 4 long calls at mark $50 = 4*50*100 = $20,000
	// total abs MV = $60,000 against $300,000 NLV = 20% (boundary case, not flagged red since > vs >=)
	pos := []model.Position{
		stockPos("TSLA", 100, 400),
		optPos("TSLA", "C", 4, 450, 50, nil),
	}
	r := Compute(pos, 300_000, DefaultConfig(), fakeSectorMap())
	if len(r.Underlyings) != 1 {
		t.Fatalf("expected 1 underlying (aggregated), got %d", len(r.Underlyings))
	}
	u := r.Underlyings[0]
	if u.Ticker != "TSLA" {
		t.Fatalf("expected TSLA, got %s", u.Ticker)
	}
	if math.Abs(u.AbsMarketValueUSD-60_000) > 0.01 {
		t.Fatalf("expected abs MV $60,000, got $%.2f", u.AbsMarketValueUSD)
	}
	if u.StockLegCount != 1 || u.OptionLegCount != 1 {
		t.Fatalf("expected 1 stock + 1 option leg, got stock=%d opt=%d", u.StockLegCount, u.OptionLegCount)
	}
	// 20% is the boundary — strictly > triggers red, so 20% itself does NOT.
	if math.Abs(u.WeightPct-20.0) > 0.01 {
		t.Fatalf("expected weight exactly 20.0%%, got %.2f", u.WeightPct)
	}
	for _, f := range r.Flags {
		if f.Level == "red" {
			t.Fatalf("expected no red flag at the 20.0%% boundary, got: %s", f.Message)
		}
	}
}

func TestCompute_ShortOptionCountedByAbsoluteValue(t *testing.T) {
	// A short put with negative MV must still count as economic exposure.
	mv := -5_000.0
	pos := []model.Position{optPos("RKLB", "P", -1, 130, 50, &mv)}
	r := Compute(pos, 100_000, DefaultConfig(), fakeSectorMap())
	u := r.Underlyings[0]
	if math.Abs(u.AbsMarketValueUSD-5_000) > 0.01 {
		t.Fatalf("abs MV should be $5,000, got $%.2f", u.AbsMarketValueUSD)
	}
	if math.Abs(u.NetMarketValueUSD-(-5_000)) > 0.01 {
		t.Fatalf("net MV preserves sign, expected -$5,000, got $%.2f", u.NetMarketValueUSD)
	}
	if math.Abs(u.WeightPct-5.0) > 0.01 {
		t.Fatalf("weight should be 5%%, got %.2f", u.WeightPct)
	}
}

func TestCompute_MissingMarkSurfaced(t *testing.T) {
	// Quantity != 0 but MV = 0 simulates an option without OPRA subscription.
	mv := 0.0
	pos := []model.Position{
		stockPos("GOOGL", 200, 380),     // good
		optPos("META", "C", 2, 850, 0, &mv), // missing mark
	}
	r := Compute(pos, 500_000, DefaultConfig(), fakeSectorMap())
	if len(r.MissingMarkTickers) != 1 || r.MissingMarkTickers[0] != "META" {
		t.Fatalf("expected MissingMarkTickers=[META], got %v", r.MissingMarkTickers)
	}
	// META still gets an UnderlyingGroup entry (so the user sees the position
	// is held) but with zero MV.
	var foundMETA bool
	for _, u := range r.Underlyings {
		if u.Ticker == "META" {
			foundMETA = true
			if !u.HasMissingMark {
				t.Fatalf("META should have HasMissingMark=true")
			}
			if u.AbsMarketValueUSD != 0 {
				t.Fatalf("META MV should be 0 (mark unavailable), got %f", u.AbsMarketValueUSD)
			}
		}
	}
	if !foundMETA {
		t.Fatalf("META should appear in Underlyings even with missing mark, got tickers %v", tickers(r.Underlyings))
	}
}

func TestCompute_FlagsRedAndYellow(t *testing.T) {
	// Build positions: GOOGL 25% (red), MSFT 15% (yellow), QQQ 5% (none).
	pos := []model.Position{
		stockPos("GOOGL", 1, 250_000), // $250k = 25% of $1M
		stockPos("MSFT", 1, 150_000),  // 15%
		stockPos("QQQ", 1, 50_000),    // 5%
	}
	r := Compute(pos, 1_000_000, DefaultConfig(), fakeSectorMap())

	var redCount, warnCount int
	var foundGOOGLRed, foundMSFTYellow bool
	for _, f := range r.Flags {
		if f.Level == "red" {
			redCount++
			if strings.Contains(f.Message, "GOOGL") {
				foundGOOGLRed = true
			}
		}
		if f.Level == "warn" {
			warnCount++
			if strings.Contains(f.Message, "MSFT") {
				foundMSFTYellow = true
			}
		}
	}
	if !foundGOOGLRed {
		t.Fatalf("GOOGL 25%% should produce a red flag; got flags=%+v", r.Flags)
	}
	if !foundMSFTYellow {
		t.Fatalf("MSFT 15%% should produce a yellow flag; got flags=%+v", r.Flags)
	}
}

func TestCompute_HHIBuckets(t *testing.T) {
	// Diversified: 10 names each at 5% → HHI = 10 × (0.05)² × 10000 = 250
	diversifiedPos := make([]model.Position, 10)
	for i := 0; i < 10; i++ {
		diversifiedPos[i] = stockPos(strings.ToUpper(string(rune('A'+i))+"X"), 1, 50_000)
	}
	r := Compute(diversifiedPos, 1_000_000, DefaultConfig(), fakeSectorMap())
	if r.HHIBucket != "diversified" {
		t.Fatalf("10 names @ 5%% should be diversified (HHI %.0f), got bucket %s", r.HHI, r.HHIBucket)
	}

	// Concentrated: 1 name at 60% → HHI = 0.6² × 10000 = 3600
	concentratedPos := []model.Position{stockPos("MEGA", 1, 600_000)}
	r2 := Compute(concentratedPos, 1_000_000, DefaultConfig(), fakeSectorMap())
	if r2.HHIBucket != "concentrated" {
		t.Fatalf("1 name @ 60%% should be concentrated (HHI %.0f), got bucket %s", r2.HHI, r2.HHIBucket)
	}
}

func TestCompute_SectorGroupingAndOrdering(t *testing.T) {
	// Mega-cap tech (GOOGL+MSFT) = 30%; AI auto (TSLA) = 10%; Aerospace (RKLB) = 5%.
	// Expected order: tech > auto > aerospace.
	pos := []model.Position{
		stockPos("GOOGL", 1, 200_000),
		stockPos("MSFT", 1, 100_000),
		stockPos("TSLA", 1, 100_000),
		stockPos("RKLB", 1, 50_000),
	}
	r := Compute(pos, 1_000_000, DefaultConfig(), fakeSectorMap())
	if len(r.Sectors) != 3 {
		t.Fatalf("expected 3 sectors, got %d (%+v)", len(r.Sectors), r.Sectors)
	}
	if r.Sectors[0].SectorID != "mega-cap-tech" {
		t.Fatalf("top sector should be mega-cap-tech, got %s", r.Sectors[0].SectorID)
	}
	if math.Abs(r.Sectors[0].WeightPct-30.0) > 0.01 {
		t.Fatalf("mega-cap-tech weight should be 30%%, got %.2f", r.Sectors[0].WeightPct)
	}
	// Tickers within the sector should be sorted alphabetically for stable diffing.
	if r.Sectors[0].Tickers[0] != "GOOGL" || r.Sectors[0].Tickers[1] != "MSFT" {
		t.Fatalf("sector tickers should be alphabetically sorted, got %v", r.Sectors[0].Tickers)
	}
}

func TestCompute_TopKRollups(t *testing.T) {
	// 3 names at 25/15/10% → Top2=40, Top5=50 (sums the 3 we have).
	pos := []model.Position{
		stockPos("A", 1, 250_000),
		stockPos("B", 1, 150_000),
		stockPos("C", 1, 100_000),
	}
	r := Compute(pos, 1_000_000, DefaultConfig(), fakeSectorMap())
	if math.Abs(r.Top2Pct-40.0) > 0.01 {
		t.Fatalf("Top2 expected 40%%, got %.2f", r.Top2Pct)
	}
	if math.Abs(r.Top5Pct-50.0) > 0.01 {
		t.Fatalf("Top5 expected 50%% (caps at portfolio size), got %.2f", r.Top5Pct)
	}
	// Top 2 = 40% > 30% warn threshold; expect at least one Top-2 warn.
	var topWarn bool
	for _, f := range r.Flags {
		if strings.Contains(f.Message, "Top 2") {
			topWarn = true
		}
	}
	if !topWarn {
		t.Fatalf("Top 2 of 40%% should trip the 30%% warn; got flags=%+v", r.Flags)
	}
}

func TestCompute_RenderRunsAndContainsKeyData(t *testing.T) {
	pos := []model.Position{
		stockPos("GOOGL", 1, 250_000),
		stockPos("MSFT", 1, 150_000),
	}
	r := Compute(pos, 1_000_000, DefaultConfig(), fakeSectorMap())
	r.NetLiqSGD = 1_276_500
	r.FXUSDtoSGD = 1.2765

	var buf bytes.Buffer
	r.Render(&buf)
	out := buf.String()
	// Sanity check: NLV header rendered with both currencies and FX.
	for _, s := range []string{"GOOGL", "MSFT", "Net Liq", "USD $1,000,000", "SGD $1,276,500", "FX 1.2765", "Top 2:", "HHI:"} {
		if !strings.Contains(out, s) {
			t.Errorf("rendered output missing %q\n---OUTPUT---\n%s", s, out)
		}
	}
}

func TestReport_JSONRoundTrip(t *testing.T) {
	pos := []model.Position{stockPos("GOOGL", 1, 250_000)}
	r := Compute(pos, 1_000_000, DefaultConfig(), fakeSectorMap())
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Report
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Underlyings) != 1 || back.Underlyings[0].Ticker != "GOOGL" {
		t.Fatalf("round-trip lost data: %+v", back)
	}
}

func tickers(ug []UnderlyingGroup) []string {
	out := make([]string, len(ug))
	for i, u := range ug {
		out[i] = u.Ticker
	}
	return out
}
