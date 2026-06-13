package premarket

import (
	"sort"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// nyLoc：America/New_York（premarket 包私有 —— 不 import internal/intel，否则与
// intel/handlers.go(import premarket) 形成循环依赖）。
var nyLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.FixedZone("EST", -5*3600)
	}
	return loc
}()

// curatedMovers：内置精选集（流动性好的盘前活跃名单，代码默认不上配置）。
var curatedMovers = []string{"AAPL", "NVDA", "MSFT", "AMZN", "META", "GOOGL", "TSLA", "SPY", "QQQ"}

// premarketWindow 返回盘前窗(04:00-09:30 ET)内 bar 的累计量与窗内最后收盘价。
func premarketWindow(bars []model.OHLCV) (vol int64, last float64) {
	ny := nyLoc
	for _, b := range bars {
		et := b.Timestamp.In(ny)
		mins := et.Hour()*60 + et.Minute()
		if mins >= 4*60 && mins < 9*60+30 {
			vol += b.Volume
			last = b.Close
		}
	}
	return vol, last
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
		s = normSym(s)
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

func normSym(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' {
			continue
		}
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		out = append(out, c)
	}
	return string(out)
}

// computeVolRatio：今日盘前窗量 / 近 N 日盘前窗均量（按交易日分组）。
func computeVolRatio(today int64, histBars []model.OHLCV) float64 {
	ny := nyLoc
	byDay := map[string]int64{}
	todayKey := time.Now().In(ny).Format("2006-01-02")
	for _, b := range histBars {
		et := b.Timestamp.In(ny)
		mins := et.Hour()*60 + et.Minute()
		if mins < 4*60 || mins >= 9*60+30 {
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
