package intraday

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/IS908/optix/internal/intelshared"
	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/pkg/model"
)

const (
	moverLimit         = 8
	barInterval        = "5 mins"
	barLookback        = 8 * time.Hour
	defaultLoadTTL     = 8 * time.Second
	defaultSnapshotTTL = 25 * time.Second
	sessionOpenHour    = 9
	sessionOpenMin     = 30

	// basisUnearned is the canonical-enum ("realtime|delayed|approx|frozen")
	// floor used whenever a load has no data to vouch for — total source
	// failure, or a nil source. It must never be "realtime": a BrokerSource's
	// nominal Basis() reflects what the source claims when healthy, not
	// whether THIS load actually succeeded, so blindly repeating it on
	// failure was the #191 finding-3 bug (header said "realtime" next to an
	// "unavailable" warning and zero data).
	basisUnearned = "delayed"
)

type MarketSource interface {
	SourceName() string
	Basis() string
	Quotes(ctx context.Context, symbols []string) (map[string]Quote, error)
	Bars(ctx context.Context, symbols []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error)
}

type snapshotSource interface {
	Snapshot(ctx context.Context, symbols []string, interval string, lookback time.Duration) (map[string]Quote, map[string][]model.OHLCV, error)
}

type Service struct {
	src          MarketSource
	sectors      *portfolio.SectorMap
	sectorSource string
	LoadTimeout  time.Duration
	// SnapshotTTL controls how long a loaded (quotes, bars) snapshot is
	// reused across calls, keyed by the resolved symbol universe. Movers and
	// SectorHeatmap are served by separate HTTP endpoints that the SPA polls
	// independently every ~30s; without this, each poll cycle opened two
	// fresh IBKR connect/disconnect round trips (one per card) instead of
	// one (#191 finding 5). <= 0 disables caching.
	SnapshotTTL time.Duration
	Now         func() time.Time

	cacheMu sync.Mutex
	cache   map[string]snapshotCacheEntry
}

type snapshotCacheEntry struct {
	at       time.Time
	quotes   map[string]Quote
	bars     map[string][]model.OHLCV
	warnings []string
}

func NewService(src MarketSource, sectors *portfolio.SectorMap, sectorSource string) *Service {
	if sectors == nil {
		sectors = &portfolio.SectorMap{}
	}
	return &Service{
		src:          src,
		sectors:      sectors,
		sectorSource: sectorSource,
		LoadTimeout:  defaultLoadTTL,
		SnapshotTTL:  defaultSnapshotTTL,
		Now:          time.Now,
	}
}

func (s *Service) Movers(ctx context.Context, watchlist []string) (MoversDTO, error) {
	asOf := s.now()
	movers, warnings, source, basis := s.computeMovers(ctx, watchlist, asOf)
	gainers, losers := splitMovers(movers)
	return MoversDTO{
		AsOf:         asOf.UTC(),
		Source:       source,
		Basis:        basis,
		UniverseNote: "watchlist plus curated liquid US names",
		Gainers:      gainers,
		Losers:       losers,
		Warnings:     warnings,
	}, nil
}

func (s *Service) SectorHeatmap(ctx context.Context, watchlist []string) (SectorHeatmapDTO, error) {
	asOf := s.now()
	movers, warnings, source, basis := s.computeMovers(ctx, watchlist, asOf)
	return SectorHeatmapDTO{
		AsOf:         asOf.UTC(),
		Source:       source,
		Basis:        basis,
		SectorSource: s.sectorSource,
		Rows:         buildSectorHeatmap(movers, s.sectors),
		Warnings:     warnings,
	}, nil
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// computeMovers loads market data for the resolved symbol universe and
// returns the movers, warnings, and the Source/Basis labels the caller
// should render at the DTO's top level.
//
// Source/Basis are resolved here (not left to the caller) because only this
// function knows whether the load actually succeeded: on total failure the
// nominal source's static Basis() (e.g. "realtime" for ibkr-preferred) must
// NOT be echoed back — that repeats an unearned claim next to an
// "unavailable" warning (#191 finding 3).
func (s *Service) computeMovers(ctx context.Context, watchlist []string, asOf time.Time) (movers []Mover, warnings []string, source string, basis string) {
	if s.src == nil {
		return []Mover{}, []string{"intraday source unavailable"}, "unavailable", basisUnearned
	}
	symbols, inWatchlist := moverUniverse(watchlist)
	warnings = []string{}
	quotes, bars, loadWarnings := s.loadMarketData(ctx, symbols)
	warnings = append(warnings, loadWarnings...)
	if len(loadWarnings) > 0 && (len(quotes) == 0 || len(bars) == 0) {
		return []Mover{}, warnings, sourceName(s.src), basisUnearned
	}

	movers = make([]Mover, 0, len(symbols))
	for _, symbol := range symbols {
		quote, ok := quotes[symbol]
		if !ok || quote.Last <= 0 {
			continue
		}
		open, volume, ok := sessionOpenAndVolume(bars[symbol], asOf)
		if !ok || open <= 0 {
			continue
		}
		pct := ((quote.Last - open) / open) * 100
		movers = append(movers, Mover{
			Symbol:    symbol,
			Source:    firstNonEmpty(quote.Source, sourceName(s.src)),
			Basis:     firstNonEmpty(quote.Basis, basisName(s.src)),
			AsOf:      nonZeroTime(quote.AsOf, asOf),
			Last:      quote.Last,
			Open:      open,
			Pct:       pct,
			Volume:    volume,
			Watchlist: inWatchlist[symbol],
		})
	}
	if len(movers) == 0 {
		warnings = append(warnings, "no intraday movers available for the current universe")
	}
	return movers, warnings, aggregateSource(movers, sourceName(s.src)), aggregateBasis(movers, basisName(s.src))
}

// loadMarketData serves a cached (quotes, bars) snapshot when one exists for
// this symbol universe within SnapshotTTL, otherwise loads fresh and caches
// the result (success or failure) for reuse (#191 finding 5).
func (s *Service) loadMarketData(ctx context.Context, symbols []string) (map[string]Quote, map[string][]model.OHLCV, []string) {
	now := s.now()
	key := snapshotCacheKey(symbols)
	if s.SnapshotTTL > 0 {
		if quotes, bars, warnings, ok := s.cachedSnapshot(key, now); ok {
			return quotes, bars, warnings
		}
	}
	quotes, bars, warnings := s.loadMarketDataFresh(ctx, symbols)
	if s.SnapshotTTL > 0 {
		s.storeSnapshot(key, now, quotes, bars, warnings)
	}
	return quotes, bars, warnings
}

func (s *Service) cachedSnapshot(key string, now time.Time) (map[string]Quote, map[string][]model.OHLCV, []string, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache[key]
	if !ok || now.Sub(entry.at) > s.SnapshotTTL {
		return nil, nil, nil, false
	}
	return entry.quotes, entry.bars, entry.warnings, true
}

func (s *Service) storeSnapshot(key string, now time.Time, quotes map[string]Quote, bars map[string][]model.OHLCV, warnings []string) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cache == nil {
		s.cache = map[string]snapshotCacheEntry{}
	}
	s.cache[key] = snapshotCacheEntry{at: now, quotes: quotes, bars: bars, warnings: warnings}
}

// snapshotCacheKey canonicalizes a symbol set into a cache key independent
// of input ordering (moverUniverse's output order is already deterministic
// given a fixed watchlist, but sorting here keeps the cache correct even if
// that ever changes).
func snapshotCacheKey(symbols []string) string {
	sorted := append([]string(nil), symbols...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

func (s *Service) loadMarketDataFresh(ctx context.Context, symbols []string) (map[string]Quote, map[string][]model.OHLCV, []string) {
	loadCtx := ctx
	var cancel context.CancelFunc
	if s.LoadTimeout > 0 {
		loadCtx, cancel = context.WithTimeout(ctx, s.LoadTimeout)
		defer cancel()
	}
	total := len(symbols)
	if snap, ok := s.src.(snapshotSource); ok {
		quotes, bars, err := snap.Snapshot(loadCtx, symbols, barInterval, barLookback)
		if err != nil {
			return nil, nil, []string{fmt.Sprintf("intraday source unavailable: %v", err)}
		}
		return quotes, bars, partialDataWarnings(sourceName(s.src), total, quotes, bars)
	}
	quotes, err := s.src.Quotes(loadCtx, symbols)
	if err != nil {
		return nil, nil, []string{fmt.Sprintf("quote source unavailable: %v", err)}
	}
	bars, err := s.src.Bars(loadCtx, symbols, barInterval, barLookback)
	if err != nil {
		return nil, nil, []string{fmt.Sprintf("bar source unavailable: %v", err)}
	}
	return quotes, bars, partialDataWarnings(sourceName(s.src), total, quotes, bars)
}

// partialDataWarnings flags both total emptiness and partial shortfalls.
// Pre-#191, a per-symbol failure inside the 8s LoadTimeout (deadline firing
// mid-loop, or a handful of individual GetQuote/GetHistoricalBars errors)
// was silently `continue`'d past with no warning at all unless EVERY symbol
// failed — so a card missing half its universe rendered as a clean, fully
// populated result (finding 2).
func partialDataWarnings(source string, total int, quotes map[string]Quote, bars map[string][]model.OHLCV) []string {
	var warnings []string
	if total == 0 {
		return warnings
	}
	if len(quotes) == 0 {
		warnings = append(warnings, fmt.Sprintf("intraday quotes empty from %s", source))
	} else if len(quotes) < total {
		warnings = append(warnings, fmt.Sprintf("%d/%d symbols unavailable for intraday quotes from %s", total-len(quotes), total, source))
	}
	if len(bars) == 0 {
		warnings = append(warnings, fmt.Sprintf("intraday bars empty from %s", source))
	} else if len(bars) < total {
		warnings = append(warnings, fmt.Sprintf("%d/%d symbols unavailable for intraday bars from %s", total-len(bars), total, source))
	}
	return warnings
}

func sessionOpenAndVolume(bars []model.OHLCV, asOf time.Time) (float64, int64, bool) {
	ny := intelshared.NY()
	asOfNY := asOf.In(ny)
	var open float64
	var volume int64
	found := false
	for _, bar := range bars {
		barNY := bar.Timestamp.In(ny)
		if barNY.Year() != asOfNY.Year() || barNY.YearDay() != asOfNY.YearDay() {
			continue
		}
		if barNY.Hour() < sessionOpenHour || (barNY.Hour() == sessionOpenHour && barNY.Minute() < sessionOpenMin) {
			continue
		}
		if barNY.After(asOfNY) {
			continue
		}
		if !found {
			open = bar.Open
			found = true
		}
		volume += bar.Volume
	}
	return open, volume, found
}

func splitMovers(movers []Mover) ([]Mover, []Mover) {
	gainers := append([]Mover(nil), movers...)
	sort.Slice(gainers, func(i, j int) bool {
		if gainers[i].Pct == gainers[j].Pct {
			return gainers[i].Symbol < gainers[j].Symbol
		}
		return gainers[i].Pct > gainers[j].Pct
	})
	losers := append([]Mover(nil), movers...)
	sort.Slice(losers, func(i, j int) bool {
		if losers[i].Pct == losers[j].Pct {
			return losers[i].Symbol < losers[j].Symbol
		}
		return losers[i].Pct < losers[j].Pct
	})
	return limitMovers(gainers, moverLimit), limitMovers(losers, moverLimit)
}

func limitMovers(movers []Mover, limit int) []Mover {
	if movers == nil {
		return []Mover{}
	}
	if len(movers) > limit {
		return movers[:limit]
	}
	return movers
}

func buildSectorHeatmap(movers []Mover, sectors *portfolio.SectorMap) []SectorHeatmapRow {
	type acc struct {
		row    SectorHeatmapRow
		sum    float64
		topAbs float64
	}
	bySector := map[string]*acc{}
	for _, mover := range movers {
		sectorID := sectors.Sector(mover.Symbol)
		a := bySector[sectorID]
		if a == nil {
			a = &acc{row: SectorHeatmapRow{SectorID: sectorID, SectorLabel: sectors.Label(sectorID)}}
			bySector[sectorID] = a
		}
		a.sum += mover.Pct
		a.row.SampleN++
		if mover.Pct >= 0 {
			a.row.Gainers++
		} else {
			a.row.Losers++
		}
		if math.Abs(mover.Pct) > a.topAbs {
			a.topAbs = math.Abs(mover.Pct)
			a.row.TopSymbol = mover.Symbol
		}
	}
	rows := make([]SectorHeatmapRow, 0, len(bySector))
	for _, a := range bySector {
		a.row.AvgPct = a.sum / float64(a.row.SampleN)
		rows = append(rows, a.row)
	}
	sort.Slice(rows, func(i, j int) bool {
		ai, aj := math.Abs(rows[i].AvgPct), math.Abs(rows[j].AvgPct)
		if ai == aj {
			return rows[i].SectorID < rows[j].SectorID
		}
		return ai > aj
	})
	return rows
}

// aggregateSource summarizes the movers' per-row Source into one top-level
// label. Source is a free-form identifier (not a constrained enum), so
// "mixed" is an honest, valid value here when the batch genuinely drew from
// more than one upstream (e.g. a future composite source) — unlike
// aggregateBasis below.
func aggregateSource(movers []Mover, fallback string) string {
	seen := map[string]bool{}
	for _, mover := range movers {
		if mover.Source != "" {
			seen[mover.Source] = true
		}
	}
	return aggregateLabel(seen, fallback)
}

// aggregateBasis summarizes the movers' per-row Basis into one top-level
// label. Basis MUST stay within the canonical enum
// (realtime|delayed|approx|frozen; internal/marketdata/source.go) — "mixed"
// is not a member of that enum, so unlike aggregateSource this picks the
// dominant (most common, ties broken alphabetically for determinism) basis
// among the movers instead of returning "mixed" (#191 finding 3). Per-row
// Basis values are untouched, so a genuinely mixed batch still shows its
// true basis per line item; only the misleading top-level label is fixed.
func aggregateBasis(movers []Mover, fallback string) string {
	counts := map[string]int{}
	for _, mover := range movers {
		if mover.Basis != "" {
			counts[mover.Basis]++
		}
	}
	return dominantLabel(counts, fallback)
}

func aggregateLabel(seen map[string]bool, fallback string) string {
	if len(seen) == 0 {
		return fallback
	}
	if len(seen) == 1 {
		for value := range seen {
			return value
		}
	}
	return "mixed"
}

func dominantLabel(counts map[string]int, fallback string) string {
	if len(counts) == 0 {
		return fallback
	}
	best, bestN := "", -1
	for value, n := range counts {
		if n > bestN || (n == bestN && value < best) {
			best, bestN = value, n
		}
	}
	return best
}

func sourceName(src MarketSource) string {
	if src == nil || src.SourceName() == "" {
		return "unavailable"
	}
	return src.SourceName()
}

// basisName returns a source's nominal basis, falling back to the
// canonical-enum floor (basisUnearned) rather than the invalid "degraded"
// sentinel when the source is nil or its basis is unset. Note this reflects
// what the source CLAIMS when healthy, not whether a particular load
// actually succeeded — computeMovers is responsible for not using this as
// the DTO-level basis on total load failure (#191 finding 3).
func basisName(src MarketSource) string {
	if src == nil || src.Basis() == "" {
		return basisUnearned
	}
	return src.Basis()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func nonZeroTime(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback.UTC()
	}
	return value.UTC()
}
