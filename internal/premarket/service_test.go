package premarket

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

type fakeSource struct {
	quotes map[string]marketdata.Quote
	daily  map[string][]model.OHLCV
	premkt map[string][]model.OHLCV
	pc     marketdata.PCRatio
	pcErr  error
}

func (f *fakeSource) Quotes(_ context.Context, ids []string) (map[string]marketdata.Quote, error) {
	out := map[string]marketdata.Quote{}
	for _, id := range ids {
		if q, ok := f.quotes[id]; ok {
			out[id] = q
		}
	}
	return out, nil
}

func (f *fakeSource) DailyBars(_ context.Context, id string, _ time.Duration) ([]model.OHLCV, error) {
	return f.daily[id], nil
}

func (f *fakeSource) PremarketBars(_ context.Context, _ []string, _ int) (map[string][]model.OHLCV, error) {
	return f.premkt, nil
}

func (f *fakeSource) PutCallRatio(_ context.Context, _ string) (marketdata.PCRatio, error) {
	return f.pc, f.pcErr
}

func newSvc(t *testing.T, src MarketSource, now time.Time) (*Service, *sqlite.Store) {
	t.Helper()
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	svc := NewService(src, s)
	svc.Now = func() time.Time { return now }
	return svc, s
}

func premarketMoverBar(y int, m time.Month, d, hour, minute int, close float64, volume int64) model.OHLCV {
	return model.OHLCV{
		Timestamp: time.Date(y, m, d, hour, minute, 0, 0, nyLoc),
		Close:     close,
		Volume:    volume,
	}
}

func TestMoversUsesTodayPremarketAgainstPriorRegularClose(t *testing.T) {
	now := time.Date(2026, time.June, 12, 9, 15, 0, 0, nyLoc)
	src := &fakeSource{premkt: map[string][]model.OHLCV{
		"AAPL": {
			premarketMoverBar(2026, time.June, 10, 4, 0, 90, 400),
			premarketMoverBar(2026, time.June, 10, 9, 0, 91, 600),
			premarketMoverBar(2026, time.June, 10, 16, 0, 95, 1_000),
			premarketMoverBar(2026, time.June, 11, 4, 0, 96, 200),
			premarketMoverBar(2026, time.June, 11, 9, 0, 97, 300),
			premarketMoverBar(2026, time.June, 11, 16, 0, 100, 1_000),
			premarketMoverBar(2026, time.June, 12, 4, 0, 108, 100),
			premarketMoverBar(2026, time.June, 12, 9, 0, 110, 200),
		},
	}}
	svc, _ := newSvc(t, src, now)

	movers, err := svc.Movers(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	if len(movers.Gainers) == 0 || movers.Gainers[0].Symbol != "AAPL" {
		t.Fatalf("gainers = %#v, want AAPL", movers.Gainers)
	}
	got := movers.Gainers[0]
	if math.Abs(got.Pct-10) > 0.001 {
		t.Fatalf("pct = %.4f, want 10.0000", got.Pct)
	}
	if math.Abs(got.VolRatio-0.4) > 0.001 {
		t.Fatalf("vol_ratio = %.4f, want 0.4000", got.VolRatio)
	}
}

func dailySeries() []model.OHLCV {
	return []model.OHLCV{
		bar(2026, 1, 2, 100, 101, 99, 100),
		bar(2026, 1, 3, 100.7, 101, 99.5, 100.8),
		bar(2026, 1, 4, 101.5, 102, 101.0, 101.8),
	}
}

func TestGapsTTLRefresh(t *testing.T) {
	src := &fakeSource{
		quotes: map[string]marketdata.Quote{"ES": {ChangePct: 0.7, Basis: marketdata.BasisDelayed}},
		daily:  map[string][]model.OHLCV{"SPX": dailySeries()},
	}
	svc, store := newSvc(t, src, time.Now().UTC())
	ctx := context.Background()

	g, err := svc.Gaps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if g.Symbol != "SPX" || g.Direction != "up" || g.Band != "0.5-1" {
		t.Errorf("implied gap (ES 0.7%%) = %+v", g)
	}
	if g.SampleN != 2 {
		t.Errorf("sample_n = %d, want 2", g.SampleN)
	}
	if len(g.ByBand) == 0 {
		t.Error("by_band should be populated")
	}

	rows, _, err := store.GetGapStats(ctx, "SPX")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Error("gap stats should be persisted")
	}
}

func TestGapsEmptyStatsReturnsArray(t *testing.T) {
	src := &fakeSource{
		quotes: map[string]marketdata.Quote{"ES": {ChangePct: 0.1, Basis: marketdata.BasisDelayed}},
		daily:  map[string][]model.OHLCV{"SPX": nil},
	}
	svc, _ := newSvc(t, src, time.Now().UTC())
	g, err := svc.Gaps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if g.ByBand == nil {
		t.Fatal("by_band must be an empty array, not null")
	}
	if len(g.ByBand) != 0 {
		t.Fatalf("by_band len = %d, want 0", len(g.ByBand))
	}
}

func TestGapsDegradesWhenStoreUnavailable(t *testing.T) {
	now := time.Now().UTC()
	src := &fakeSource{
		quotes: map[string]marketdata.Quote{"ES": {ChangePct: -0.8, Basis: marketdata.BasisDelayed}},
	}
	svc := NewService(src, nil)
	svc.Now = func() time.Time { return now }

	g, err := svc.Gaps(context.Background())

	if err != nil {
		t.Fatalf("Gaps returned error: %v", err)
	}
	if g.Symbol != "SPX" || g.ByBand == nil {
		t.Fatalf("degraded gaps should keep stable shape, got %+v", g)
	}
	if g.Direction != "down" || g.Band != "0.5-1" {
		t.Fatalf("degraded gaps should still expose implied ES gap, got %+v", g)
	}
	if len(g.Warnings) == 0 {
		t.Fatal("degraded gaps should carry warning")
	}
}

func TestBundleIsolatesFailure(t *testing.T) {
	src := &fakeSource{
		quotes: map[string]marketdata.Quote{
			"VIX": {Price: 20}, "VIX3M": {Price: 22},
			"N225": {ChangePct: 1.0}, "TSMC_TW": {ChangePct: 0.8},
			"SX5E": {ChangePct: 0.5}, "ES": {ChangePct: 0.3},
		},
		daily: map[string][]model.OHLCV{"SPX": dailySeries()},
		pcErr: errors.New("option chain down"),
	}
	svc, _ := newSvc(t, src, time.Now().UTC())
	b, err := svc.Bundle(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Sentiment.PCAvailable {
		t.Error("P/C should be unavailable")
	}
	if b.Sentiment.VIXTermPremium <= 1 {
		t.Errorf("VIX premium still computed (contango), got %v", b.Sentiment.VIXTermPremium)
	}
	if len(b.Sentiment.Warnings) == 0 {
		t.Error("sentiment should carry a warning")
	}
	if b.Overnight.Consistency.Total != 4 {
		t.Errorf("overnight still works, total=%d", b.Overnight.Consistency.Total)
	}
}

func TestBundleDegradesWhenGapsStoreUnavailable(t *testing.T) {
	src := &fakeSource{
		quotes: map[string]marketdata.Quote{
			"VIX": {Price: 20}, "VIX3M": {Price: 22},
			"N225": {ChangePct: 1.0}, "TSMC_TW": {ChangePct: 0.8},
			"SX5E": {ChangePct: 0.5}, "ES": {ChangePct: 0.3},
		},
	}
	svc := NewService(src, nil)
	svc.Now = func() time.Time { return time.Now().UTC() }

	b, err := svc.Bundle(context.Background(), nil)

	if err != nil {
		t.Fatalf("Bundle returned error: %v", err)
	}
	if len(b.Gaps.Warnings) == 0 {
		t.Fatal("bundle gaps should degrade with warning")
	}
	if b.Overnight.Consistency.Total != 4 {
		t.Errorf("overnight should still work, total=%d", b.Overnight.Consistency.Total)
	}
}

func TestSentimentWarnsMissingVixLegs(t *testing.T) {
	src := &fakeSource{
		quotes: map[string]marketdata.Quote{"VIX": {Price: 20}},
		pc:     marketdata.PCRatio{PCOI: 1.0, PCVol: 0.9},
	}
	svc, _ := newSvc(t, src, time.Now().UTC())
	got, err := svc.Sentiment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.VIXTermPremium != 0 {
		t.Errorf("term premium = %v, want 0 when VIX3M is missing", got.VIXTermPremium)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("expected missing VIX3M warning")
	}
}

func TestSentimentMissingDataDoesNotCreateBullishSignal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		quotes map[string]marketdata.Quote
		pc     marketdata.PCRatio
		pcErr  error
		want   string
	}{
		{"all missing", nil, marketdata.PCRatio{}, errors.New("unavailable"), "不可用"},
		{"missing PC with backwardation", map[string]marketdata.Quote{"VIX": {Price: 25}, "VIX3M": {Price: 20}}, marketdata.PCRatio{}, errors.New("unavailable"), "防御"},
		{"missing PC with flat term", map[string]marketdata.Quote{"VIX": {Price: 20}, "VIX3M": {Price: 20}}, marketdata.PCRatio{}, errors.New("unavailable"), "中性"},
		{"valid zero PC", nil, marketdata.PCRatio{PCOI: 0}, nil, "偏多"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newSvc(t, &fakeSource{quotes: tc.quotes, pc: tc.pc, pcErr: tc.pcErr}, time.Now())
			got, err := svc.Sentiment(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got.Regime != tc.want {
				t.Fatalf("regime = %q, want %q", got.Regime, tc.want)
			}
		})
	}
}
