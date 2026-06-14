package postclose

import (
	"sort"
	"time"

	"github.com/IS908/optix/internal/intelshared"
	"github.com/IS908/optix/pkg/model"
)

var nyLoc = intelshared.NY()

var curatedUniverse = []string{"AAPL", "NVDA", "MSFT", "AMZN", "META", "GOOGL", "TSLA", "AVGO", "AMD", "SPY", "QQQ"}

func Universe(watchlist []string) (symbols []string, inWL map[string]bool) {
	inWL = map[string]bool{}
	seen := map[string]bool{}
	add := func(s string, wl bool) {
		s = intelshared.NormalizeSymbol(s)
		if s == "" {
			return
		}
		if wl {
			inWL[s] = true
		}
		if seen[s] {
			return
		}
		seen[s] = true
		symbols = append(symbols, s)
	}
	for _, s := range watchlist {
		add(s, true)
	}
	for _, s := range curatedUniverse {
		add(s, false)
	}
	return symbols, inWL
}

func ExtractPostcloseMoves(bySymbol map[string][]model.OHLCV, watchlist map[string]bool, now time.Time) ([]Mover, []string) {
	out := []Mover{}
	warnings := []string{}
	for sym, bars := range bySymbol {
		symbol := intelshared.NormalizeSymbol(sym)
		if symbol == "" {
			continue
		}
		move, ok, warning := extractPostcloseMove(symbol, bars, watchlist[symbol], now)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if ok {
			out = append(out, move)
		}
	}
	sort.Slice(out, func(i, j int) bool { return abs(out[i].CombinedPct) > abs(out[j].CombinedPct) })
	return out, warnings
}

func extractPostcloseMove(symbol string, bars []model.OHLCV, inWatchlist bool, now time.Time) (Mover, bool, string) {
	if len(bars) == 0 {
		return Mover{}, false, ""
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Timestamp.Before(bars[j].Timestamp) })
	latestDay := ""
	for _, b := range bars {
		et := b.Timestamp.In(nyLoc)
		if et.After(now.In(nyLoc)) {
			continue
		}
		day := et.Format("2006-01-02")
		if isRegular(et) || isPostclose(et) {
			latestDay = day
		}
	}
	if latestDay == "" {
		return Mover{}, false, ""
	}
	prevClose := 0.0
	regularClose := 0.0
	afterClose := 0.0
	hasAfter := false
	var volume int64
	for _, b := range bars {
		et := b.Timestamp.In(nyLoc)
		day := et.Format("2006-01-02")
		if day < latestDay && isRegular(et) {
			prevClose = b.Close
		}
		if day != latestDay {
			continue
		}
		if isRegular(et) {
			regularClose = b.Close
			volume += b.Volume
		}
		if isPostclose(et) {
			afterClose = b.Close
			hasAfter = true
			volume += b.Volume
		}
	}
	if prevClose <= 0 || regularClose <= 0 {
		return Mover{}, false, ""
	}
	warning := ""
	if !hasAfter || afterClose <= 0 {
		afterClose = regularClose
		warning = symbol + ": 盘后 bar 缺席，使用常规收盘价"
	}
	return Mover{
		Symbol:        symbol,
		RegularPct:    (regularClose - prevClose) / prevClose * 100,
		AfterHoursPct: (afterClose - regularClose) / regularClose * 100,
		CombinedPct:   (afterClose - prevClose) / prevClose * 100,
		Volume:        volume,
		Watchlist:     inWatchlist,
	}, true, warning
}

func isRegular(t time.Time) bool {
	mins := t.Hour()*60 + t.Minute()
	return mins >= 9*60+30 && mins <= 16*60
}

func isPostclose(t time.Time) bool {
	mins := t.Hour()*60 + t.Minute()
	return mins > 16*60 && mins < 20*60
}

func rankPostcloseMovers(in []Mover) (gainers, losers []Mover) {
	for _, m := range in {
		if m.CombinedPct >= 0 {
			gainers = append(gainers, m)
		} else {
			losers = append(losers, m)
		}
	}
	sort.Slice(gainers, func(i, j int) bool { return gainers[i].CombinedPct > gainers[j].CombinedPct })
	sort.Slice(losers, func(i, j int) bool { return losers[i].CombinedPct < losers[j].CombinedPct })
	const top = 8
	if len(gainers) > top {
		gainers = gainers[:top]
	}
	if len(losers) > top {
		losers = losers[:top]
	}
	return gainers, losers
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
