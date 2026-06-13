package premarket

import (
	"context"
	"fmt"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
	"golang.org/x/sync/singleflight"
)

const (
	gapLookbackDays = 504
	gapTTL          = 18 * time.Hour
	headlineSymbol  = "SPX"
)

var overnightChain = []struct {
	ID    string
	Label string
}{
	{"N225", "东京 N225"},
	{"TSMC_TW", "台北 台积电"},
	{"SX5E", "欧洲 SX5E"},
	{"ES", "美期 ES"},
}

type MarketSource interface {
	Quotes(ctx context.Context, ids []string) (map[string]marketdata.Quote, error)
	DailyBars(ctx context.Context, id string, lookback time.Duration) ([]model.OHLCV, error)
	PremarketBars(ctx context.Context, rawSymbols []string, days int) (map[string][]model.OHLCV, error)
	PutCallRatio(ctx context.Context, underlying string) (marketdata.PCRatio, error)
}

type Service struct {
	src   MarketSource
	store *sqlite.Store
	sf    singleflight.Group
	Now   func() time.Time
}

func NewService(src MarketSource, store *sqlite.Store) *Service {
	return &Service{src: src, store: store}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) Overnight(ctx context.Context) (OvernightDTO, error) {
	ids := make([]string, len(overnightChain))
	for i, c := range overnightChain {
		ids[i] = c.ID
	}
	out := OvernightDTO{AsOf: s.now().UTC(), Links: []OvernightLink{}}
	quotes, err := s.src.Quotes(ctx, ids)
	if err != nil {
		out.Warnings = append(out.Warnings, "overnight: "+err.Error())
		return out, nil
	}
	pcts := make([]float64, 0, len(ids))
	for _, c := range overnightChain {
		q, ok := quotes[c.ID]
		if !ok {
			out.Warnings = append(out.Warnings, c.ID+": 取数缺席")
			continue
		}
		out.Links = append(out.Links, OvernightLink{
			ID: c.ID, Label: c.Label, Pct: q.ChangePct, Basis: string(q.Basis), AsOf: q.AsOf,
		})
		pcts = append(pcts, q.ChangePct)
	}
	same, total, note := chainConsistency(pcts)
	out.Consistency = Consistency{SameDir: same, Total: total, Note: note}
	return out, nil
}

func (s *Service) Gaps(ctx context.Context) (GapsDTO, error) {
	if s.store == nil {
		return GapsDTO{}, fmt.Errorf("premarket gap store unavailable")
	}
	rows, asOf, err := s.store.GetGapStats(ctx, headlineSymbol)
	if err != nil {
		return GapsDTO{}, err
	}
	out := GapsDTO{
		Symbol: headlineSymbol, ByBand: rows, LookbackDays: gapLookbackDays, AsOf: s.now().UTC(),
	}
	if out.ByBand == nil {
		out.ByBand = []model.PremarketGapStat{}
	}
	stale := len(rows) == 0 || s.now().Sub(asOf) > gapTTL
	if stale {
		v, _, _ := s.sf.Do("gaps:"+headlineSymbol, func() (any, error) {
			bars, ferr := s.src.DailyBars(ctx, headlineSymbol, time.Duration(gapLookbackDays)*24*time.Hour)
			if ferr != nil {
				return ferr, nil
			}
			if len(bars) < 2 {
				return fmt.Errorf("insufficient daily bars for %s", headlineSymbol), nil
			}
			fresh := ComputeGapStats(headlineSymbol, bars, gapLookbackDays)
			if err := s.store.UpsertGapStats(ctx, fresh); err != nil {
				return err, nil
			}
			return fresh, nil
		})
		switch nr := v.(type) {
		case []model.PremarketGapStat:
			if len(nr) > 0 {
				rows = nr
				out.ByBand = rows
			}
		case error:
			out.Warnings = append(out.Warnings, "gaps: "+nr.Error())
		}
	}
	if len(rows) == 0 {
		out.Warnings = append(out.Warnings, "gaps: 历史统计不可用")
	}
	quotes, qerr := s.src.Quotes(ctx, []string{"ES"})
	if qerr != nil {
		out.Warnings = append(out.Warnings, "gaps: ES 隐含开盘不可用")
		return out, nil
	}
	es, ok := quotes["ES"]
	if !ok {
		out.Warnings = append(out.Warnings, "gaps: ES 取数缺席")
		return out, nil
	}
	dir, band, gap := ImpliedGap(es.ChangePct)
	out.ImpliedGapPct, out.Direction, out.Band = gap, dir, band
	for _, r := range rows {
		if r.Direction == dir && r.Band == band {
			out.HistFillRate, out.SampleN = r.FillRate, r.SampleN
			break
		}
	}
	return out, nil
}

func (s *Service) Movers(ctx context.Context, watchlist []string) (MoversDTO, error) {
	out := MoversDTO{
		AsOf:         s.now().UTC(),
		UniverseNote: "仅自选股 + 内置精选集内排名，非全市场扫描",
		Gainers:      []Mover{},
		Losers:       []Mover{},
	}
	syms, inWL := moverUniverse(watchlist)
	bars, err := s.src.PremarketBars(ctx, syms, 5)
	if err != nil {
		out.Warnings = append(out.Warnings, "movers: "+err.Error())
		return out, nil
	}
	var inputs []moverInput
	for _, sym := range syms {
		series := bars[sym]
		if len(series) == 0 {
			continue
		}
		todayVol, last := premarketWindow(series)
		if last <= 0 {
			continue
		}
		prevClose := priorClose(series, last)
		if prevClose <= 0 {
			continue
		}
		inputs = append(inputs, moverInput{
			Symbol:      sym,
			Pct:         (last - prevClose) / prevClose * 100,
			VolRatio:    computeVolRatio(todayVol, series),
			InWatchlist: inWL[sym],
		})
	}
	g, l := rankMovers(inputs)
	out.Gainers = toMovers(g)
	out.Losers = toMovers(l)
	if len(out.Gainers) == 0 && len(out.Losers) == 0 {
		out.Warnings = append(out.Warnings, "movers: 盘前无数据（非交易时段/周末）")
	}
	return out, nil
}

func priorClose(series []model.OHLCV, fallback float64) float64 {
	if len(series) > 0 && series[0].Close > 0 {
		return series[0].Close
	}
	return fallback
}

func toMovers(in []moverInput) []Mover {
	out := make([]Mover, 0, len(in))
	for _, m := range in {
		out = append(out, Mover{
			Symbol: m.Symbol, Pct: m.Pct, VolRatio: m.VolRatio, Watchlist: m.InWatchlist,
		})
	}
	return out
}

func (s *Service) Sentiment(ctx context.Context) (SentimentDTO, error) {
	out := SentimentDTO{
		AsOf:         s.now().UTC(),
		DegradedNote: "降级口径：VIX 期限用指数族比值，情绪为当下定位非历史分位",
	}
	if quotes, err := s.src.Quotes(ctx, []string{"VIX", "VIX3M"}); err == nil {
		if vix, ok := quotes["VIX"]; ok {
			out.VIX = vix.Price
		} else {
			out.Warnings = append(out.Warnings, "sentiment: VIX 取数缺席")
		}
		if vix3m, ok := quotes["VIX3M"]; ok {
			out.VIX3M = vix3m.Price
		} else {
			out.Warnings = append(out.Warnings, "sentiment: VIX3M 取数缺席")
		}
		out.VIXTermPremium = vixTermPremium(out.VIX, out.VIX3M)
	} else {
		out.Warnings = append(out.Warnings, "sentiment: VIX 取数失败")
	}
	pc, err := s.src.PutCallRatio(ctx, "SPX")
	if err != nil {
		out.Warnings = append(out.Warnings, "sentiment: P/C 不可用（期权链取数失败）")
	} else {
		out.PCAvailable = true
		out.PCOI, out.PCVol = pc.PCOI, pc.PCVol
	}
	out.Regime = regimeLabel(out.PCOI, out.VIXTermPremium)
	return out, nil
}

func (s *Service) Bundle(ctx context.Context, watchlist []string) (BundleDTO, error) {
	ov, _ := s.Overnight(ctx)
	gp, err := s.Gaps(ctx)
	if err != nil {
		return BundleDTO{}, fmt.Errorf("gaps: %w", err)
	}
	mv, _ := s.Movers(ctx, watchlist)
	st, _ := s.Sentiment(ctx)
	return BundleDTO{Overnight: ov, Gaps: gp, Movers: mv, Sentiment: st}, nil
}
