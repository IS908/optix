package portfolio

import (
	"math"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func TestComputeHistoricalBeta(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	spyReturns := []float64{0.01, -0.02, 0.015, 0.005, -0.01}
	symReturns := make([]float64, len(spyReturns))
	for i, r := range spyReturns {
		symReturns[i] = 1.5 * r
	}

	beta, obs, asOf, err := ComputeHistoricalBeta(barsFromReturns(base, 100, symReturns), barsFromReturns(base, 400, spyReturns), 3, 60)
	if err != nil {
		t.Fatalf("ComputeHistoricalBeta: %v", err)
	}
	if obs != len(spyReturns) {
		t.Fatalf("obs = %d, want %d", obs, len(spyReturns))
	}
	if math.Abs(beta-1.5) > 1e-9 {
		t.Fatalf("beta = %v, want 1.5", beta)
	}
	if asOf.IsZero() {
		t.Fatal("asOf should be set")
	}
}

func TestComputeHistoricalBetaRequiresMinimumObservations(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _, _, err := ComputeHistoricalBeta(
		barsFromReturns(base, 100, []float64{0.01}),
		barsFromReturns(base, 400, []float64{0.01}),
		3,
		60,
	)
	if err == nil {
		t.Fatal("expected min observation error")
	}
}

func TestComputeHistoricalBetaUsesLatestMaxObservations(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	spyReturns := []float64{0.01, -0.01, 0.02, -0.02, 0.015}
	symReturns := []float64{0.50, -0.40, 2 * 0.02, 2 * -0.02, 2 * 0.015}

	beta, obs, asOf, err := ComputeHistoricalBeta(
		barsFromReturns(base, 100, symReturns),
		barsFromReturns(base, 400, spyReturns),
		3,
		3,
	)
	if err != nil {
		t.Fatalf("ComputeHistoricalBeta: %v", err)
	}
	if obs != 3 {
		t.Fatalf("obs = %d, want 3", obs)
	}
	if math.Abs(beta-2.0) > 1e-9 {
		t.Fatalf("beta = %v, want latest-window beta 2.0", beta)
	}
	wantAsOf := base.AddDate(0, 0, len(spyReturns))
	if !asOf.Equal(wantAsOf) {
		t.Fatalf("asOf = %s, want %s", asOf, wantAsOf)
	}
}

func TestComputeHistoricalBetaSortsInputBars(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	spyBars := barsFromReturns(base, 400, []float64{0.01, -0.02, 0.015, 0.005, -0.01})
	symBars := barsFromReturns(base, 100, []float64{0.015, -0.03, 0.0225, 0.0075, -0.015})

	shuffleBarsForTest(spyBars)
	shuffleBarsForTest(symBars)
	beta, _, _, err := ComputeHistoricalBeta(symBars, spyBars, 3, 60)
	if err != nil {
		t.Fatalf("ComputeHistoricalBeta: %v", err)
	}
	if math.Abs(beta-1.5) > 1e-9 {
		t.Fatalf("beta = %v, want 1.5 from sorted returns", beta)
	}
}

func barsFromReturns(base time.Time, start float64, returns []float64) []model.OHLCV {
	out := []model.OHLCV{{Timestamp: base, Close: start}}
	close := start
	for i, r := range returns {
		close *= 1 + r
		out = append(out, model.OHLCV{Timestamp: base.AddDate(0, 0, i+1), Close: close})
	}
	return out
}

func shuffleBarsForTest(bars []model.OHLCV) {
	for i, j := 0, len(bars)-1; i < j; i, j = i+1, j-1 {
		bars[i], bars[j] = bars[j], bars[i]
	}
}
