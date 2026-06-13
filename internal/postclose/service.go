package postclose

import (
	"context"
	"time"

	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/pkg/model"
)

const (
	earningsLimit = 12
	marketDays    = 5
)

type MarketSource interface {
	Earnings(ctx context.Context, symbols []string, limit int) (map[string][]marketdata.EarningsEvent, error)
	PostcloseBars(ctx context.Context, symbols []string, days int) (map[string][]model.OHLCV, error)
}

type Service struct {
	src          MarketSource
	sectors      *portfolio.SectorMap
	sectorSource string
	Now          func() time.Time
}

func NewService(src MarketSource, sectors *portfolio.SectorMap, sectorSource string) *Service {
	if sectors == nil {
		sectors = &portfolio.SectorMap{SectorLabels: map[string]string{}, TickerSectors: map[string]string{}}
	}
	if sectorSource == "" {
		sectorSource = "<unknown>"
	}
	return &Service{src: src, sectors: sectors, sectorSource: sectorSource}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) Earnings(ctx context.Context, watchlist []string) (EarningsDTO, error) {
	out := EarningsDTO{
		AsOf:         s.now().UTC(),
		Source:       "yfinance earnings_dates (EPS-only degraded consensus)",
		UniverseNote: "自选股 + 内置精选集；EPS 共识为免费 yfinance 降级口径",
		Reports:      []EarningsReport{},
	}
	symbols, _ := Universe(watchlist)
	raw, err := s.src.Earnings(ctx, symbols, earningsLimit)
	if err != nil {
		out.Warnings = append(out.Warnings, "earnings: "+err.Error())
		return out, nil
	}
	out.Reports = BuildEarningsReports(raw, s.now().UTC(), 14*24*time.Hour, 30*24*time.Hour)
	if len(out.Reports) == 0 {
		out.Warnings = append(out.Warnings, "earnings: 当前 universe 内无近期财报行")
	}
	return out, nil
}

func (s *Service) Movers(ctx context.Context, watchlist []string) (MoversDTO, error) {
	out := MoversDTO{
		AsOf:         s.now().UTC(),
		UniverseNote: "自选股 + 内置精选集；排名依据常规盘+盘后合并变化",
		Gainers:      []Mover{},
		Losers:       []Mover{},
	}
	symbols, inWL := Universe(watchlist)
	bars, err := s.src.PostcloseBars(ctx, symbols, marketDays)
	if err != nil {
		out.Warnings = append(out.Warnings, "movers: "+err.Error())
		return out, nil
	}
	moves, warnings := ExtractPostcloseMoves(bars, inWL, s.now())
	out.Warnings = append(out.Warnings, warnings...)
	out.Gainers, out.Losers = rankPostcloseMovers(moves)
	if len(out.Gainers) == 0 && len(out.Losers) == 0 {
		out.Warnings = append(out.Warnings, "movers: 收盘后 bar 不足")
	}
	return out, nil
}

func (s *Service) ReadAcross(ctx context.Context, watchlist []string) (ReadAcrossDTO, error) {
	out := ReadAcrossDTO{
		AsOf:         s.now().UTC(),
		SectorSource: s.sectorSource,
		Edges:        []ReadAcrossEdge{},
	}
	movers, _ := s.Movers(ctx, watchlist)
	all := append(append([]Mover{}, movers.Gainers...), movers.Losers...)
	out.Warnings = append(out.Warnings, movers.Warnings...)
	out.Edges = BuildReadAcrossEdges(all, s.sectors)
	if len(out.Edges) == 0 {
		out.Warnings = append(out.Warnings, "read_across: 未触发同板块传导边")
	}
	return out, nil
}

func (s *Service) Timeline(ctx context.Context, watchlist []string) (TimelineDTO, error) {
	out := TimelineDTO{AsOf: s.now().UTC(), Events: []TimelineEvent{}}
	earnings, _ := s.Earnings(ctx, watchlist)
	movers, _ := s.Movers(ctx, watchlist)
	readAcross, _ := s.ReadAcross(ctx, watchlist)
	out.Warnings = append(out.Warnings, earnings.Warnings...)
	out.Warnings = append(out.Warnings, movers.Warnings...)
	out.Warnings = append(out.Warnings, readAcross.Warnings...)
	allMoves := append(append([]Mover{}, movers.Gainers...), movers.Losers...)
	out.Events = BuildTimeline(s.now().UTC(), earnings.Reports, allMoves, readAcross.Edges)
	if len(out.Events) == 0 {
		out.Warnings = append(out.Warnings, "timeline: 暂无结构化收盘后事件")
	}
	return out, nil
}

func (s *Service) Bundle(ctx context.Context, watchlist []string) (BundleDTO, error) {
	earnings, _ := s.Earnings(ctx, watchlist)
	movers, _ := s.Movers(ctx, watchlist)
	readAcross, _ := s.ReadAcross(ctx, watchlist)
	timeline := TimelineDTO{AsOf: s.now().UTC(), Events: []TimelineEvent{}}
	allMoves := append(append([]Mover{}, movers.Gainers...), movers.Losers...)
	timeline.Events = BuildTimeline(s.now().UTC(), earnings.Reports, allMoves, readAcross.Edges)
	timeline.Warnings = append(timeline.Warnings, earnings.Warnings...)
	timeline.Warnings = append(timeline.Warnings, movers.Warnings...)
	timeline.Warnings = append(timeline.Warnings, readAcross.Warnings...)
	if len(timeline.Events) == 0 {
		timeline.Warnings = append(timeline.Warnings, "timeline: 暂无结构化收盘后事件")
	}
	return BundleDTO{Earnings: earnings, Timeline: timeline, ReadAcross: readAcross, Movers: movers}, nil
}
