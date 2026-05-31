package portfolio

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestRunStressUsesDeltaGammaAndVIXShocks(t *testing.T) {
	report := &GreeksReport{
		SnapshotAt: time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC),
		NetLiqUSD:  100_000,
		Groups: []GreeksGroup{
			{Key: "AAPL", DollarDelta: 1000, Gamma: 50, Vega: -20},
			{Key: "TSLA", DollarDelta: -500, Gamma: 10, Vega: -100},
		},
	}
	scenarios := []StressScenario{{
		ID:    "spy-down-vix-up",
		Label: "SPY down, VIX up",
		Shocks: []StressShock{
			{Axis: "spy_pct", Magnitude: -0.03},
			{Axis: "vix_pct", Magnitude: 0.50},
		},
	}}

	out := RunStress(report, scenarios)
	if len(out.Scenarios) != 1 {
		t.Fatalf("scenarios = %+v", out.Scenarios)
	}
	got := out.Scenarios[0]
	// Linearized P&L per group:
	// AAPL: 1000*-3 + 0.5*50*9 + (-20*50) = -3775
	// TSLA: -500*-3 + 0.5*10*9 + (-100*50) = -3455
	want := -7230.0
	if math.Abs(got.TotalPnLUSD-want) > 1e-9 {
		t.Fatalf("TotalPnLUSD = %v, want %v", got.TotalPnLUSD, want)
	}
	if math.Abs(got.PctNLV-(-7.23)) > 1e-9 {
		t.Fatalf("PctNLV = %v, want -7.23", got.PctNLV)
	}
	if got.WorstPosition.Key != "AAPL" || got.WorstPosition.PnLUSD != -3775 {
		t.Fatalf("worst = %+v", got.WorstPosition)
	}
}

func TestRenderStressIncludesWorstTail(t *testing.T) {
	report := RunStress(&GreeksReport{NetLiqUSD: 100_000}, []StressScenario{{
		ID: "spy-down-3", Label: "SPY -3%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.03}},
	}})
	var b strings.Builder
	RenderStress(report, &b)
	out := b.String()
	if !strings.Contains(out, "PORTFOLIO STRESS TEST") || !strings.Contains(out, "SPY -3%") || !strings.Contains(out, "Worst tail") {
		t.Fatalf("render output missing expected text:\n%s", out)
	}
}
