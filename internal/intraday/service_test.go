package intraday

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/pkg/model"
)

type fakeSource struct {
	quotes map[string]Quote
	bars   map[string][]model.OHLCV
	err    error
}

func (f fakeSource) SourceName() string { return "ibkr" }

func (f fakeSource) Basis() string { return "realtime" }

func (f fakeSource) Quotes(_ context.Context, symbols []string) (map[string]Quote, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]Quote{}
	for _, symbol := range symbols {
		if q, ok := f.quotes[symbol]; ok {
			out[symbol] = q
		}
	}
	return out, nil
}

func (f fakeSource) Bars(_ context.Context, symbols []string, _ string, _ time.Duration) (map[string][]model.OHLCV, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string][]model.OHLCV{}
	for _, symbol := range symbols {
		if bars, ok := f.bars[symbol]; ok {
			out[symbol] = bars
		}
	}
	return out, nil
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
}

func bar(ts string, open, close float64, volume int64) model.OHLCV {
	t, _ := time.Parse(time.RFC3339, ts)
	return model.OHLCV{Timestamp: t, Open: open, High: close, Low: open, Close: close, Volume: volume}
}

func testSectors() *portfolio.SectorMap {
	return &portfolio.SectorMap{
		SectorLabels: map[string]string{"mega-cap-tech": "Mega-cap Tech", "etf": "ETF"},
		TickerSectors: map[string]string{
			"AAPL": "mega-cap-tech",
			"MSFT": "mega-cap-tech",
			"SPY":  "etf",
		},
	}
}

func TestMoversRanksGainersAndLosersFromSessionOpen(t *testing.T) {
	svc := NewService(fakeSource{
		quotes: map[string]Quote{
			"AAPL": {Symbol: "AAPL", Last: 110, AsOf: fixedNow()},
			"MSFT": {Symbol: "MSFT", Last: 95, AsOf: fixedNow()},
		},
		bars: map[string][]model.OHLCV{
			"AAPL": {bar("2026-06-25T13:30:00Z", 100, 101, 1000), bar("2026-06-25T14:00:00Z", 101, 109, 1500)},
			"MSFT": {bar("2026-06-25T13:30:00Z", 100, 99, 900), bar("2026-06-25T14:00:00Z", 99, 96, 1100)},
		},
	}, testSectors(), "<test>")
	svc.Now = fixedNow

	dto, err := svc.Movers(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dto.Gainers) == 0 || dto.Gainers[0].Symbol != "AAPL" {
		t.Fatalf("gainers = %#v, want AAPL first", dto.Gainers)
	}
	if len(dto.Losers) == 0 || dto.Losers[0].Symbol != "MSFT" {
		t.Fatalf("losers = %#v, want MSFT first", dto.Losers)
	}
	if got := dto.Gainers[0].Pct; got < 9.9 || got > 10.1 {
		t.Fatalf("AAPL pct = %.2f, want about 10", got)
	}
	if !dto.Gainers[0].Watchlist {
		t.Fatalf("AAPL watchlist = false, want true")
	}
	if dto.Source != "ibkr" || dto.Basis != "realtime" {
		t.Fatalf("source/basis = %s/%s, want ibkr/realtime", dto.Source, dto.Basis)
	}
}

func TestMoversDegradesWithWarningsAndNonNilSlices(t *testing.T) {
	svc := NewService(fakeSource{err: errors.New("ibkr down")}, testSectors(), "<test>")
	svc.Now = fixedNow

	dto, err := svc.Movers(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	if dto.Gainers == nil || dto.Losers == nil {
		t.Fatalf("movers arrays must be non-nil: %+v", dto)
	}
	if len(dto.Warnings) == 0 {
		t.Fatalf("warnings empty, want source failure warning")
	}
}

func TestMoversWarnsWhenSourceReturnsNoQuotesOrBars(t *testing.T) {
	svc := NewService(fakeSource{quotes: map[string]Quote{}, bars: map[string][]model.OHLCV{}}, testSectors(), "<test>")
	svc.Now = fixedNow

	dto, err := svc.Movers(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	if dto.Gainers == nil || dto.Losers == nil {
		t.Fatalf("movers arrays must be non-nil: %+v", dto)
	}
	if len(dto.Warnings) < 2 {
		t.Fatalf("warnings = %#v, want empty quote/bar warnings", dto.Warnings)
	}
}

func TestSectorHeatmapAggregatesMoverRows(t *testing.T) {
	svc := NewService(fakeSource{
		quotes: map[string]Quote{
			"AAPL": {Symbol: "AAPL", Last: 110, AsOf: fixedNow()},
			"MSFT": {Symbol: "MSFT", Last: 98, AsOf: fixedNow()},
			"SPY":  {Symbol: "SPY", Last: 505, AsOf: fixedNow()},
		},
		bars: map[string][]model.OHLCV{
			"AAPL": {bar("2026-06-25T13:30:00Z", 100, 101, 1000)},
			"MSFT": {bar("2026-06-25T13:30:00Z", 100, 99, 1000)},
			"SPY":  {bar("2026-06-25T13:30:00Z", 500, 502, 1000)},
		},
	}, testSectors(), "<test>")
	svc.Now = fixedNow

	dto, err := svc.SectorHeatmap(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(dto.Rows) == 0 {
		t.Fatalf("heatmap rows empty")
	}
	tech := findSector(t, dto.Rows, "mega-cap-tech")
	if tech.SampleN != 2 || tech.Gainers != 1 || tech.Losers != 1 {
		t.Fatalf("tech row = %+v, want sample=2 gainers=1 losers=1", tech)
	}
	if tech.TopSymbol != "AAPL" {
		t.Fatalf("top symbol = %s, want AAPL", tech.TopSymbol)
	}
}

func TestMoversCapsGainersAndLosersAtEightRows(t *testing.T) {
	quotes := map[string]Quote{}
	bars := map[string][]model.OHLCV{}
	watchlist := []string{}
	for i := 0; i < 18; i++ {
		symbol := string(rune('A'+i)) + "CAP"
		watchlist = append(watchlist, symbol)
		last := 90.0 + float64(i)
		if i >= 9 {
			last = 101.0 + float64(i)
		}
		quotes[symbol] = Quote{Symbol: symbol, Last: last, AsOf: fixedNow()}
		bars[symbol] = []model.OHLCV{bar("2026-06-25T13:30:00Z", 100, 100, 1000)}
	}
	svc := NewService(fakeSource{quotes: quotes, bars: bars}, testSectors(), "<test>")
	svc.Now = fixedNow

	dto, err := svc.Movers(context.Background(), watchlist)
	if err != nil {
		t.Fatal(err)
	}
	if len(dto.Gainers) != 8 || len(dto.Losers) != 8 {
		t.Fatalf("gainers/losers lengths = %d/%d, want 8/8", len(dto.Gainers), len(dto.Losers))
	}
}

type fakeSnapshotSource struct {
	fakeSource
	snapshots  int
	quoteCalls int
	barCalls   int
}

func (f *fakeSnapshotSource) Snapshot(ctx context.Context, symbols []string, interval string, lookback time.Duration) (map[string]Quote, map[string][]model.OHLCV, error) {
	f.snapshots++
	quotes, err := f.fakeSource.Quotes(ctx, symbols)
	if err != nil {
		return nil, nil, err
	}
	bars, err := f.fakeSource.Bars(ctx, symbols, interval, lookback)
	return quotes, bars, err
}

func (f *fakeSnapshotSource) Quotes(ctx context.Context, symbols []string) (map[string]Quote, error) {
	f.quoteCalls++
	return f.fakeSource.Quotes(ctx, symbols)
}

func (f *fakeSnapshotSource) Bars(ctx context.Context, symbols []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error) {
	f.barCalls++
	return f.fakeSource.Bars(ctx, symbols, interval, lookback)
}

func TestMoversUsesSnapshotSourceWhenAvailable(t *testing.T) {
	src := &fakeSnapshotSource{fakeSource: fakeSource{
		quotes: map[string]Quote{"AAPL": {Symbol: "AAPL", Last: 110, AsOf: fixedNow()}},
		bars: map[string][]model.OHLCV{
			"AAPL": {bar("2026-06-25T13:30:00Z", 100, 101, 1000)},
		},
	}}
	svc := NewService(src, testSectors(), "<test>")
	svc.Now = fixedNow

	if _, err := svc.Movers(context.Background(), []string{"AAPL"}); err != nil {
		t.Fatal(err)
	}
	if src.snapshots != 1 || src.quoteCalls != 0 || src.barCalls != 0 {
		t.Fatalf("snapshots/quoteCalls/barCalls = %d/%d/%d, want 1/0/0", src.snapshots, src.quoteCalls, src.barCalls)
	}
}

type blockingSnapshotSource struct{}

func (blockingSnapshotSource) SourceName() string { return "ibkr-preferred" }

func (blockingSnapshotSource) Basis() string { return "realtime" }

func (blockingSnapshotSource) Quotes(context.Context, []string) (map[string]Quote, error) {
	return nil, errors.New("unexpected direct quote call")
}

func (blockingSnapshotSource) Bars(context.Context, []string, string, time.Duration) (map[string][]model.OHLCV, error) {
	return nil, errors.New("unexpected direct bar call")
}

func (blockingSnapshotSource) Snapshot(ctx context.Context, _ []string, _ string, _ time.Duration) (map[string]Quote, map[string][]model.OHLCV, error) {
	<-ctx.Done()
	return nil, nil, ctx.Err()
}

func TestMoversTimesOutBlockedSourcesWithWarnings(t *testing.T) {
	svc := NewService(blockingSnapshotSource{}, testSectors(), "<test>")
	svc.Now = fixedNow
	svc.LoadTimeout = 10 * time.Millisecond

	start := time.Now()
	dto, err := svc.Movers(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("blocked source took too long")
	}
	if dto.Gainers == nil || dto.Losers == nil {
		t.Fatalf("movers arrays must be non-nil: %+v", dto)
	}
	if len(dto.Warnings) == 0 {
		t.Fatalf("warnings empty, want timeout warning")
	}
}

// TestMoversReportsDelayedBasisOnTotalSourceFailure is the #191 finding-3
// regression test: on total load failure, the DTO's top-level Basis must not
// repeat the source's nominal "realtime" claim next to an "unavailable"
// warning — that's what the pre-fix code did, since basisName(s.src) simply
// echoed the (still-nominal-realtime) BrokerSource.Basis() regardless of
// whether the load actually happened.
func TestMoversReportsDelayedBasisOnTotalSourceFailure(t *testing.T) {
	svc := NewService(fakeSource{err: errors.New("ibkr down")}, testSectors(), "<test>")
	svc.Now = fixedNow

	dto, err := svc.Movers(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	if dto.Basis == "realtime" {
		t.Fatalf("basis = realtime on total load failure — must not claim realtime with zero data")
	}
	if dto.Basis != "delayed" {
		t.Fatalf("basis = %s, want the canonical-enum honest floor (delayed)", dto.Basis)
	}
}

func TestSectorHeatmapReportsDelayedBasisOnTotalSourceFailure(t *testing.T) {
	svc := NewService(fakeSource{err: errors.New("ibkr down")}, testSectors(), "<test>")
	svc.Now = fixedNow

	dto, err := svc.SectorHeatmap(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	if dto.Basis == "realtime" {
		t.Fatalf("basis = realtime on total load failure — must not claim realtime with zero data")
	}
}

func TestMoversNilSourceReportsUnavailableSourceAndHonestBasis(t *testing.T) {
	svc := NewService(nil, testSectors(), "<test>")
	svc.Now = fixedNow

	dto, err := svc.Movers(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if dto.Source != "unavailable" {
		t.Fatalf("source = %s, want unavailable", dto.Source)
	}
	if dto.Basis == "realtime" || dto.Basis == "degraded" {
		t.Fatalf("basis = %s, want a canonical non-realtime basis (never the invalid 'degraded' sentinel)", dto.Basis)
	}
}

// TestAggregateBasisPicksDominantInsteadOfMixed is the other half of finding
// 3: "mixed" is not a member of the canonical basis enum
// (realtime|delayed|approx|frozen), so the top-level aggregate must pick the
// dominant per-row basis instead of returning the literal string "mixed".
func TestAggregateBasisPicksDominantInsteadOfMixed(t *testing.T) {
	movers := []Mover{{Basis: "realtime"}, {Basis: "realtime"}, {Basis: "delayed"}}

	got := aggregateBasis(movers, "delayed")

	if got == "mixed" {
		t.Fatalf("aggregateBasis returned the invalid enum value %q", got)
	}
	if got != "realtime" {
		t.Fatalf("aggregateBasis = %s, want dominant value realtime", got)
	}
}

// TestMoversWarnsWithCountWhenSomeSymbolsUnavailable is the #191 finding-2
// regression test: pre-fix, emptyDataWarnings only fired when the ENTIRE
// quotes/bars map came back empty, so a partial per-symbol shortfall (some
// GetQuote/GetHistoricalBars calls failing, or the 8s LoadTimeout firing
// mid-loop) rendered as a clean, silently-truncated result.
func TestMoversWarnsWithCountWhenSomeSymbolsUnavailable(t *testing.T) {
	svc := NewService(fakeSource{
		quotes: map[string]Quote{"AAPL": {Symbol: "AAPL", Last: 110, AsOf: fixedNow()}},
		bars: map[string][]model.OHLCV{
			"AAPL": {bar("2026-06-25T13:30:00Z", 100, 101, 1000)},
		},
	}, testSectors(), "<test>")
	svc.Now = fixedNow

	// curatedMovers alone is a 9-symbol universe; only AAPL has data, so 8/9
	// symbols should be reported unavailable for both quotes and bars.
	dto, err := svc.Movers(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range dto.Warnings {
		if strings.Contains(w, "8/9") && strings.Contains(w, "unavailable") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v, want an \"8/9 ... unavailable\" style partial-failure warning", dto.Warnings)
	}
}

// TestMoversLeavesVolumeZeroWhenBarVolumeAbsent is the #191 finding-4
// regression test: substituting the daily quote volume for a missing
// session-bar volume mixes two different measures under one label. When bar
// volume is absent, Volume must stay 0 (existing warnings already surface
// the gap) rather than being silently backfilled from quote.Volume.
func TestMoversLeavesVolumeZeroWhenBarVolumeAbsent(t *testing.T) {
	svc := NewService(fakeSource{
		quotes: map[string]Quote{"AAPL": {Symbol: "AAPL", Last: 110, Volume: 99999, AsOf: fixedNow()}},
		bars: map[string][]model.OHLCV{
			"AAPL": {bar("2026-06-25T13:30:00Z", 100, 101, 0)},
		},
	}, testSectors(), "<test>")
	svc.Now = fixedNow

	dto, err := svc.Movers(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dto.Gainers) == 0 {
		t.Fatalf("gainers empty, want AAPL")
	}
	if dto.Gainers[0].Volume != 0 {
		t.Fatalf("volume = %d, want 0 (must not backfill from the unrelated daily quote volume 99999)", dto.Gainers[0].Volume)
	}
}

// TestLoadMarketDataReusesSnapshotWithinTTL is the #191 finding-5 regression
// test: Movers and SectorHeatmap are separate HTTP endpoints, but within one
// poll cycle they should share a single loaded snapshot instead of each
// opening its own broker session.
func TestLoadMarketDataReusesSnapshotWithinTTL(t *testing.T) {
	src := &fakeSnapshotSource{fakeSource: fakeSource{
		quotes: map[string]Quote{"AAPL": {Symbol: "AAPL", Last: 110, AsOf: fixedNow()}},
		bars: map[string][]model.OHLCV{
			"AAPL": {bar("2026-06-25T13:30:00Z", 100, 101, 1000)},
		},
	}}
	svc := NewService(src, testSectors(), "<test>")
	clock := fixedNow()
	svc.Now = func() time.Time { return clock }
	svc.SnapshotTTL = 25 * time.Second

	if _, err := svc.Movers(context.Background(), []string{"AAPL"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SectorHeatmap(context.Background(), []string{"AAPL"}); err != nil {
		t.Fatal(err)
	}
	if src.snapshots != 1 {
		t.Fatalf("snapshots = %d, want 1 (Movers and SectorHeatmap should share the cached snapshot)", src.snapshots)
	}
}

// TestLoadMarketDataRefreshesSnapshotAfterTTL locks the other half: the
// cache must not go stale forever — once the injectable clock advances past
// SnapshotTTL, the next call re-fetches.
func TestLoadMarketDataRefreshesSnapshotAfterTTL(t *testing.T) {
	src := &fakeSnapshotSource{fakeSource: fakeSource{
		quotes: map[string]Quote{"AAPL": {Symbol: "AAPL", Last: 110, AsOf: fixedNow()}},
		bars: map[string][]model.OHLCV{
			"AAPL": {bar("2026-06-25T13:30:00Z", 100, 101, 1000)},
		},
	}}
	svc := NewService(src, testSectors(), "<test>")
	clock := fixedNow()
	svc.Now = func() time.Time { return clock }
	svc.SnapshotTTL = 25 * time.Second

	if _, err := svc.Movers(context.Background(), []string{"AAPL"}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(26 * time.Second)
	if _, err := svc.Movers(context.Background(), []string{"AAPL"}); err != nil {
		t.Fatal(err)
	}
	if src.snapshots != 2 {
		t.Fatalf("snapshots = %d, want 2 (cache must refresh once TTL elapses)", src.snapshots)
	}
}

func findSector(t *testing.T, rows []SectorHeatmapRow, id string) SectorHeatmapRow {
	t.Helper()
	for _, row := range rows {
		if row.SectorID == id {
			return row
		}
	}
	t.Fatalf("sector %q not found in %#v", id, rows)
	return SectorHeatmapRow{}
}
