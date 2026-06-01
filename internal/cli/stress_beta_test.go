package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/pkg/model"
)

func TestBuildStressBetaProviderUsesFreshCachedBeta(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := &fakeStressBetaStore{
		fresh: map[string]model.SymbolBeta{
			"AAPL": {Symbol: "AAPL", Beta: 1.42, Observations: 60, AsOf: now.Add(-24 * time.Hour), UpdatedAt: now},
		},
	}
	bars := &fakeStressBarsProvider{}

	provider := buildStressBetaProvider(context.Background(), store, bars, []portfolio.GreeksGroup{
		{Key: "AAPL", DollarDelta: 1000},
	}, now, nil)

	got, ok := provider.BetaForStress("aapl")
	if !ok {
		t.Fatal("expected cached beta provider entry")
	}
	if got.Beta != 1.42 || got.Source != "historical_cache" || got.Observations != 60 {
		t.Fatalf("beta source = %+v, want cached beta 1.42", got)
	}
	if len(bars.calls) != 0 {
		t.Fatalf("historical bars fetched despite fresh cache: %+v", bars.calls)
	}
}

func TestBuildStressBetaProviderComputesAndStoresMissingBeta(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := &fakeStressBetaStore{fresh: map[string]model.SymbolBeta{}}
	spyBars := barsFromReturnsForCLI(now.AddDate(0, 0, -30), []float64{0.01, -0.005, 0.007, 0.003, -0.002, 0.004, 0.006, -0.003, 0.005, 0.002, -0.004, 0.006, 0.001, -0.002, 0.003, 0.004, -0.001, 0.005, -0.003, 0.002, 0.004, -0.002, 0.006, 0.001, -0.004, 0.003, 0.002, -0.001, 0.005, 0.004})
	aaplBars := barsScaledFromBenchmarkForCLI(spyBars, 1.5)
	bars := &fakeStressBarsProvider{bars: map[string][]model.OHLCV{
		"SPY":  spyBars,
		"AAPL": aaplBars,
	}}

	provider := buildStressBetaProvider(context.Background(), store, bars, []portfolio.GreeksGroup{
		{Key: "AAPL", DollarDelta: 1000},
	}, now, nil)

	got, ok := provider.BetaForStress("AAPL")
	if !ok {
		t.Fatal("expected computed beta provider entry")
	}
	if got.Source != "historical_computed" || got.Observations != 30 {
		t.Fatalf("beta source = %+v, want computed with 30 observations", got)
	}
	if got.Beta < 1.49 || got.Beta > 1.51 {
		t.Fatalf("beta = %v, want ~1.5", got.Beta)
	}
	if len(store.upserts) != 1 || store.upserts[0].Symbol != "AAPL" {
		t.Fatalf("upserts = %+v, want one AAPL beta", store.upserts)
	}
}

func TestBuildStressBetaProviderContinuesCacheLookupsAfterSPYFetchFailure(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := &fakeStressBetaStore{
		fresh: map[string]model.SymbolBeta{
			"MSFT": {Symbol: "MSFT", Beta: 1.11, Observations: 55, AsOf: now.Add(-24 * time.Hour), UpdatedAt: now},
		},
	}
	bars := &fakeStressBarsProvider{errs: map[string]error{"SPY": errors.New("market unavailable")}}

	provider := buildStressBetaProvider(context.Background(), store, bars, []portfolio.GreeksGroup{
		{Key: "AAPL", DollarDelta: 1000},
		{Key: "MSFT", DollarDelta: 1000},
	}, now, nil)

	got, ok := provider.BetaForStress("MSFT")
	if !ok {
		t.Fatal("expected later cached beta after SPY fetch failure")
	}
	if got.Beta != 1.11 || got.Source != "historical_cache" {
		t.Fatalf("beta source = %+v, want cached MSFT beta", got)
	}
}

func TestBuildStressBetaProviderSkipsComputedBetaFromStaleBars(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := &fakeStressBetaStore{fresh: map[string]model.SymbolBeta{}}
	staleStart := now.Add(-45 * 24 * time.Hour)
	spyBars := barsFromReturnsForCLI(staleStart, []float64{0.01, -0.005, 0.007, 0.003, -0.002, 0.004, 0.006, -0.003, 0.005, 0.002, -0.004, 0.006, 0.001, -0.002, 0.003, 0.004, -0.001, 0.005, -0.003, 0.002, 0.004, -0.002, 0.006, 0.001, -0.004, 0.003, 0.002, -0.001, 0.005, 0.004})
	aaplBars := barsScaledFromBenchmarkForCLI(spyBars, 1.5)
	bars := &fakeStressBarsProvider{bars: map[string][]model.OHLCV{
		"SPY":  spyBars,
		"AAPL": aaplBars,
	}}

	provider := buildStressBetaProvider(context.Background(), store, bars, []portfolio.GreeksGroup{
		{Key: "AAPL", DollarDelta: 1000},
	}, now, nil)

	if _, ok := provider.BetaForStress("AAPL"); ok {
		t.Fatal("expected stale computed beta to be skipped")
	}
	if len(store.upserts) != 0 {
		t.Fatalf("upserts = %+v, want none for stale bars", store.upserts)
	}
}

type fakeStressBetaStore struct {
	fresh   map[string]model.SymbolBeta
	upserts []model.SymbolBeta
}

func (f *fakeStressBetaStore) GetFreshSymbolBeta(_ context.Context, symbol string, _ time.Duration, _ time.Time) (model.SymbolBeta, bool, error) {
	b, ok := f.fresh[symbol]
	return b, ok, nil
}

func (f *fakeStressBetaStore) UpsertSymbolBeta(_ context.Context, b model.SymbolBeta) error {
	f.upserts = append(f.upserts, b)
	return nil
}

type fakeStressBarsProvider struct {
	bars  map[string][]model.OHLCV
	errs  map[string]error
	calls []string
}

func (f *fakeStressBarsProvider) GetHistoricalBars(_ context.Context, symbol, _ string, _ int) ([]model.OHLCV, error) {
	f.calls = append(f.calls, symbol)
	if err := f.errs[symbol]; err != nil {
		return nil, err
	}
	return f.bars[symbol], nil
}

func barsFromReturnsForCLI(start time.Time, returns []float64) []model.OHLCV {
	price := 100.0
	bars := []model.OHLCV{{Timestamp: start, Close: price}}
	for i, r := range returns {
		price *= 1 + r
		bars = append(bars, model.OHLCV{Timestamp: start.AddDate(0, 0, i+1), Close: price})
	}
	return bars
}

func barsScaledFromBenchmarkForCLI(benchmark []model.OHLCV, scale float64) []model.OHLCV {
	out := make([]model.OHLCV, len(benchmark))
	out[0] = model.OHLCV{Timestamp: benchmark[0].Timestamp, Close: 100}
	for i := 1; i < len(benchmark); i++ {
		ret := benchmark[i].Close/benchmark[i-1].Close - 1
		out[i] = model.OHLCV{Timestamp: benchmark[i].Timestamp, Close: out[i-1].Close * (1 + ret*scale)}
	}
	return out
}
