package premarket

import (
	"sort"
	"time"

	"github.com/IS908/optix/internal/intelshared"
	"github.com/IS908/optix/pkg/model"
)

// nyLoc：America/New_York（经 leaf package 共享，避免 import internal/intel 形成循环）。
var nyLoc = intelshared.NY()

// curatedMovers：内置精选集（流动性好的盘前活跃名单，代码默认不上配置）。
var curatedMovers = []string{"AAPL", "NVDA", "MSFT", "AMZN", "META", "GOOGL", "TSLA", "SPY", "QQQ"}

// premarketWindow 返回盘前窗(04:00-09:30 ET)内 bar 的累计量与窗内最后收盘价。
func premarketWindow(bars []model.OHLCV) (vol int64, last float64) {
	ny := nyLoc
	for _, b := range bars {
		et := b.Timestamp.In(ny)
		if isPremarket(et) {
			vol += b.Volume
			last = b.Close
		}
	}
	return vol, last
}

func premarketWindowForDay(bars []model.OHLCV, now time.Time) (vol int64, last float64) {
	todayKey := now.In(nyLoc).Format("2006-01-02")
	for _, b := range bars {
		et := b.Timestamp.In(nyLoc)
		if et.Format("2006-01-02") != todayKey || !isPremarket(et) {
			continue
		}
		vol += b.Volume
		last = b.Close
	}
	return vol, last
}

func priorRegularCloseBefore(bars []model.OHLCV, now time.Time, fallback float64) float64 {
	todayKey := now.In(nyLoc).Format("2006-01-02")
	close := 0.0
	var closeAt time.Time
	for _, b := range bars {
		if b.Close <= 0 {
			continue
		}
		et := b.Timestamp.In(nyLoc)
		if et.Format("2006-01-02") >= todayKey || !isRegular(et) {
			continue
		}
		if closeAt.IsZero() || et.After(closeAt) {
			close = b.Close
			closeAt = et
		}
	}
	if close > 0 {
		return close
	}
	return fallback
}

type moverInput struct {
	Symbol      string
	Pct         float64 // 盘前 %变化（窗内最后价 vs 前收）
	VolRatio    float64 // 今日盘前窗量 / 近 5 日盘前窗均量
	InWatchlist bool
}

// rankMovers 按 |Pct| 排序，分涨/跌两栏（各取 top）。
func rankMovers(in []moverInput) (gainers, losers []moverInput) {
	for _, m := range in {
		if m.Pct >= 0 {
			gainers = append(gainers, m)
		} else {
			losers = append(losers, m)
		}
	}
	sort.Slice(gainers, func(i, j int) bool { return gainers[i].Pct > gainers[j].Pct })
	sort.Slice(losers, func(i, j int) bool { return losers[i].Pct < losers[j].Pct })
	const top = 8
	if len(gainers) > top {
		gainers = gainers[:top]
	}
	if len(losers) > top {
		losers = losers[:top]
	}
	return gainers, losers
}

// moverUniverse 合并 watchlist 与内置精选集（去重，标记自选）。
func moverUniverse(watchlist []string) (symbols []string, inWL map[string]bool) {
	inWL = map[string]bool{}
	seen := map[string]bool{}
	add := func(s string, wl bool) {
		s = intelshared.NormalizeSymbol(s)
		if s == "" || seen[s] {
			if wl {
				inWL[s] = true
			}
			return
		}
		seen[s] = true
		symbols = append(symbols, s)
		if wl {
			inWL[s] = true
		}
	}
	for _, s := range watchlist {
		add(s, true)
	}
	for _, s := range curatedMovers {
		add(s, false)
	}
	return symbols, inWL
}

// computeVolRatio：今日盘前窗量 / 近 N 日盘前窗均量（按交易日分组）。
func computeVolRatio(today int64, histBars []model.OHLCV, now time.Time) float64 {
	byDay := map[string]int64{}
	todayKey := now.In(nyLoc).Format("2006-01-02")
	for _, b := range histBars {
		et := b.Timestamp.In(nyLoc)
		if !isPremarket(et) {
			continue
		}
		day := et.Format("2006-01-02")
		if day == todayKey {
			continue // 今日不计入基线
		}
		byDay[day] += b.Volume
	}
	if len(byDay) == 0 || today <= 0 {
		return 0
	}
	var sum int64
	for _, v := range byDay {
		sum += v
	}
	avg := float64(sum) / float64(len(byDay))
	if avg <= 0 {
		return 0
	}
	return float64(today) / avg
}

func isPremarket(t time.Time) bool {
	mins := t.Hour()*60 + t.Minute()
	return mins >= 4*60 && mins < 9*60+30
}

func isRegular(t time.Time) bool {
	mins := t.Hour()*60 + t.Minute()
	return mins >= 9*60+30 && mins <= 16*60
}
