// Package premarket 是 Market Intel 盘前视图的分析平面：隔夜传导链、隐含开盘+跳空回补
// 统计、盘前异动+量比、情绪定位。optix 纯计算（零 LLM、零调度），消费 marketdata 取数。
package premarket

import (
	"math"
	"sort"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// gapBands 幅度档下界（%，绝对值）；<minGap 不算跳空。
const minGap = 0.25

// classifyGap 把跳空 %（带符号）分类为 (方向, 档, 是否跳空)。
func classifyGap(gapPct float64) (direction, band string, ok bool) {
	a := math.Abs(gapPct)
	if a < minGap {
		return "", "", false
	}
	dir := "up"
	if gapPct < 0 {
		dir = "down"
	}
	switch {
	case a < 0.5:
		band = "0.25-0.5"
	case a < 1.0:
		band = "0.5-1"
	default:
		band = "1+"
	}
	return dir, band, true
}

// ImpliedGap：给定 ES 隔夜涨跌%（≈ SPX 隐含跳空%，降级恒等），返回方向/档/隐含跳空%。
func ImpliedGap(esPct float64) (direction, band string, impliedGapPct float64) {
	dir, b, _ := classifyGap(esPct)
	return dir, b, esPct
}

// ComputeGapStats 纯函数：从日线 OHLC 逐日算跳空回补，按 (方向×档) 聚合 fill_rate + sample_n。
// 跳空 = (今开−昨收)/昨收；回补 up 看 今低≤昨收、down 看 今高≥昨收。<0.25% 不计。
func ComputeGapStats(symbol string, bars []model.OHLCV, lookbackDays int) []model.PremarketGapStat {
	b := append([]model.OHLCV(nil), bars...)
	sort.Slice(b, func(i, j int) bool { return b[i].Timestamp.Before(b[j].Timestamp) })

	type agg struct{ filled, total int }
	buckets := map[string]*agg{} // key = direction+"/"+band
	for i := 1; i < len(b); i++ {
		prevClose := b[i-1].Close
		if prevClose <= 0 {
			continue
		}
		gapPct := (b[i].Open - prevClose) / prevClose * 100
		dir, band, ok := classifyGap(gapPct)
		if !ok {
			continue
		}
		key := dir + "/" + band
		if buckets[key] == nil {
			buckets[key] = &agg{}
		}
		buckets[key].total++
		filled := false
		if dir == "up" {
			filled = b[i].Low <= prevClose
		} else {
			filled = b[i].High >= prevClose
		}
		if filled {
			buckets[key].filled++
		}
	}

	now := time.Now().UTC()
	out := make([]model.PremarketGapStat, 0, len(buckets))
	for key, a := range buckets {
		dir, band := splitKey(key)
		rate := 0.0
		if a.total > 0 {
			rate = float64(a.filled) / float64(a.total)
		}
		out = append(out, model.PremarketGapStat{
			Symbol: symbol, Direction: dir, Band: band, FillRate: rate,
			SampleN: a.total, LookbackDays: lookbackDays, AsOf: now,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Direction != out[j].Direction {
			return out[i].Direction < out[j].Direction
		}
		return out[i].Band < out[j].Band
	})
	return out
}

func splitKey(key string) (dir, band string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}
