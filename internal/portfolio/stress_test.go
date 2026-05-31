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
	// AAPL: 1000*-3 + 0.5*25*9 + (-20*5) = -2987.5
	// TSLA: -500*-3 + 0.5*5*9 + (-100*5) = 1022.5
	want := -1965.0
	if math.Abs(got.TotalPnLUSD-want) > 1e-9 {
		t.Fatalf("TotalPnLUSD = %v, want %v", got.TotalPnLUSD, want)
	}
	if math.Abs(got.PctNLV-(-1.965)) > 1e-9 {
		t.Fatalf("PctNLV = %v, want -1.965", got.PctNLV)
	}
	if got.WorstPosition.Key != "AAPL" || got.WorstPosition.PnLUSD != -2987.5 {
		t.Fatalf("worst = %+v", got.WorstPosition)
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
