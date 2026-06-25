package intraday

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/IS908/optix/internal/intelshared"
	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/pkg/model"
)

const (
	moverLimit      = 5
	barInterval     = "5 mins"
	barLookback     = 8 * time.Hour
	sessionOpenHour = 9
	sessionOpenMin  = 30
)

type MarketSource interface {
	SourceName() string
	Basis() string
	Quotes(ctx context.Context, symbols []string) (map[string]Quote, error)
	Bars(ctx context.Context, symbols []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error)
}

type Service struct {
	src          MarketSource
	sectors      *portfolio.SectorMap
	sectorSource string
	Now          func() time.Time
}

func NewService(src MarketSource, sectors *portfolio.SectorMap, sectorSource string) *Service {
	if sectors == nil {
		sectors = &portfolio.SectorMap{}
	}
	return &Service{
		src:          src,
		sectors:      sectors,
		sectorSource: sectorSource,
		Now:          time.Now,
	}
}

func (s *Service) Movers(ctx context.Context, watchlist []string) (MoversDTO, error) {
	asOf := s.now()
	movers, warnings := s.computeMovers(ctx, watchlist, asOf)
	gainers, losers := splitMovers(movers)
	return MoversDTO{
		AsOf:         asOf.UTC(),
		Source:       aggregateSource(movers, sourceName(s.src)),
		Basis:        aggregateBasis(movers, basisName(s.src)),
		UniverseNote: "watchlist plus curated liquid US names",
		Gainers:      gainers,
		Losers:       losers,
		Warnings:     warnings,
	}, nil
}

func (s *Service) SectorHeatmap(ctx context.Context, watchlist []string) (SectorHeatmapDTO, error) {
	asOf := s.now()
	movers, warnings := s.computeMovers(ctx, watchlist, asOf)
	return SectorHeatmapDTO{
		AsOf:         asOf.UTC(),
		Source:       aggregateSource(movers, sourceName(s.src)),
		Basis:        aggregateBasis(movers, basisName(s.src)),
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

func (s *Service) computeMovers(ctx context.Context, watchlist []string, asOf time.Time) ([]Mover, []string) {
	if s.src == nil {
		return []Mover{}, []string{"intraday source unavailable"}
	}
	symbols, inWatchlist := moverUniverse(watchlist)
	warnings := []string{}
	quotes, err := s.src.Quotes(ctx, symbols)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("quote source unavailable: %v", err))
		return []Mover{}, warnings
	}
	bars, err := s.src.Bars(ctx, symbols, barInterval, barLookback)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("bar source unavailable: %v", err))
		return []Mover{}, warnings
	}

	movers := make([]Mover, 0, len(symbols))
	for _, symbol := range symbols {
		quote, ok := quotes[symbol]
		if !ok || quote.Last <= 0 {
			continue
		}
		open, volume, ok := sessionOpenAndVolume(bars[symbol], asOf)
		if !ok || open <= 0 {
			continue
		}
		if volume == 0 {
			volume = quote.Volume
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
	return movers, warnings
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

func aggregateSource(movers []Mover, fallback string) string {
	seen := map[string]bool{}
	for _, mover := range movers {
		if mover.Source != "" {
			seen[mover.Source] = true
		}
	}
	return aggregateLabel(seen, fallback)
}

func aggregateBasis(movers []Mover, fallback string) string {
	seen := map[string]bool{}
	for _, mover := range movers {
		if mover.Basis != "" {
			seen[mover.Basis] = true
		}
	}
	return aggregateLabel(seen, fallback)
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

func sourceName(src MarketSource) string {
	if src == nil || src.SourceName() == "" {
		return "unavailable"
	}
	return src.SourceName()
}

func basisName(src MarketSource) string {
	if src == nil || src.Basis() == "" {
		return "degraded"
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
