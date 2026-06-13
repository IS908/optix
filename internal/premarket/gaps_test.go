package premarket

import (
	"math"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func bar(y, m, d int, o, h, l, c float64) model.OHLCV {
	return model.OHLCV{Timestamp: time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC), Open: o, High: h, Low: l, Close: c}
}

func TestComputeGapStats(t *testing.T) {
	// 序列：每天的 (开,高,低,收)。跳空 = 今开 vs 昨收。
	bars := []model.OHLCV{
		bar(2026, 1, 2, 100, 101, 99, 100), // day0 基准，收 100
		// day1：开 100.7（+0.7% 上跳，0.5-1 档），低 99.5≤100 → 回补 hit
		bar(2026, 1, 3, 100.7, 101, 99.5, 100.8),
		// day2：开 101.5（昨收 100.8 → +0.69% 0.5-1 上跳），低 101.0>100.8 → 未回补 miss
		bar(2026, 1, 4, 101.5, 102, 101.0, 101.8),
		// day3：开 101.0（昨收 101.8 → -0.79% 0.5-1 下跳），高 101.9≥101.8 → 回补 hit
		bar(2026, 1, 5, 101.0, 101.9, 100.5, 101.2),
	}
	stats := ComputeGapStats("SPX", bars, 504)
	// 找 up/0.5-1：2 样本(day1 hit、day2 miss) → fill_rate 0.5
	got := map[string]model.PremarketGapStat{}
	for _, s := range stats {
		got[s.Direction+"/"+s.Band] = s
	}
	up := got["up/0.5-1"]
	if up.SampleN != 2 || math.Abs(up.FillRate-0.5) > 1e-9 {
		t.Errorf("up/0.5-1 = n%d rate%v, want n2 rate0.5", up.SampleN, up.FillRate)
	}
	down := got["down/0.5-1"]
	if down.SampleN != 1 || down.FillRate != 1.0 {
		t.Errorf("down/0.5-1 = n%d rate%v, want n1 rate1.0", down.SampleN, down.FillRate)
	}
	if up.Symbol != "SPX" || up.LookbackDays != 504 {
		t.Errorf("meta = %+v", up)
	}
}

func TestGapBandClassify(t *testing.T) {
	cases := []struct {
		gapPct float64
		dir    string
		band   string
		ok     bool
	}{
		{0.1, "", "", false}, // <0.25% 不算跳空
		{0.3, "up", "0.25-0.5", true},
		{0.7, "up", "0.5-1", true},
		{1.5, "up", "1+", true},
		{-0.6, "down", "0.5-1", true},
		{-2.0, "down", "1+", true},
		{0.25, "up", "0.25-0.5", true}, // 边界含下界
	}
	for _, c := range cases {
		dir, band, ok := classifyGap(c.gapPct)
		if ok != c.ok || (ok && (dir != c.dir || band != c.band)) {
			t.Errorf("classifyGap(%v) = (%s,%s,%v), want (%s,%s,%v)", c.gapPct, dir, band, ok, c.dir, c.band, c.ok)
		}
	}
}

func TestImpliedGap(t *testing.T) {
	// 隐含开盘 = 昨收×(1+ES%)；implied_gap_pct ≈ ES%
	dir, band, gap := ImpliedGap(0.7)
	if dir != "up" || band != "0.5-1" || math.Abs(gap-0.7) > 1e-9 {
		t.Errorf("ImpliedGap(0.7) = (%s,%s,%v)", dir, band, gap)
	}
}
