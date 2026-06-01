package portfolio

import (
	"math"
	"strings"
	"testing"
	"time"
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
