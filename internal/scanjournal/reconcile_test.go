package scanjournal

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

type fakeBars struct{ bars map[string][]model.OHLCV }

func (f fakeBars) DailyBars(_ context.Context, symbols []string, _ string) (map[string][]model.OHLCV, error) {
	out := map[string][]model.OHLCV{}
	for _, s := range symbols {
		if b, ok := f.bars[s]; ok {
			out[s] = b
		}
	}
	return out, nil
}

func dailyBar(date string, close, low float64) model.OHLCV {
	t, _ := time.Parse("2006-01-02", date)
	return model.OHLCV{Timestamp: t.Add(21 * time.Hour), Close: close, Low: low} // 21:00 UTC ≈ NY 收盘后
}

func seedCandidate(fs *fakeStore, scanDate, symbol, expiry string, strike, bid float64) model.ScanCandidate {
	c := sampleModelCandidate(scanDate, symbol, expiry, strike, bid)
	fs.cands = append(fs.cands, c)
	return c
}

func sampleModelCandidate(scanDate, symbol, expiry string, strike, bid float64) model.ScanCandidate {
	return model.ScanCandidate{
		CandidateID: CandidateID(scanDate, symbol, expiry, strike),
		ScanDate:    scanDate, Rank: 1, Symbol: symbol, Right: "P", Expiry: expiry,
		DTE: 9, Strike: strike, Spot: strike * 1.08, Bid: bid, Score: 1.0,
		SymbolSource: "test", CreatedAt: time.Now().UTC(),
	}
}

func reconcileService(fs *fakeStore, bars map[string][]model.OHLCV, today string) *Service {
	svc := NewService(fs, fakeBars{bars: bars})
	svc.Now = func() time.Time {
		t, _ := time.Parse("2006-01-02", today)
		return t.Add(16 * time.Hour) // UTC 正午后，NY 同日
	}
	return svc
}

func TestReconcileHitMissAndTouched(t *testing.T) {
	fs := newFakeStore()
	// hit：到期收盘 150 > strike 145；期内低点 146 未触及
	seedCandidate(fs, "2026-07-20", "AAA", "2026-07-24", 145, 2.0)
	// miss：到期收盘 88 ≤ strike 90；期内低点 85 → touched，breach (90-85)/90=5.56%
	seedCandidate(fs, "2026-07-20", "BBB", "2026-07-24", 90, 3.0)
	bars := map[string][]model.OHLCV{
		"AAA": {dailyBar("2026-07-20", 155, 150), dailyBar("2026-07-22", 152, 146), dailyBar("2026-07-24", 150, 148)},
		"BBB": {dailyBar("2026-07-20", 95, 92), dailyBar("2026-07-22", 89, 85), dailyBar("2026-07-24", 88, 86)},
	}
	svc := reconcileService(fs, bars, "2026-07-27")
	res, err := svc.Reconcile(context.Background())
	if err != nil || res.Settled != 2 || res.Void != 0 || res.Pending != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	byID := map[string]SettledRow{}
	for _, r := range res.Results {
		byID[r.Symbol] = r
	}
	a := byID["AAA"]
	if a.Outcome != "hit" || a.ExpiryClose != 150 || a.RealizedPnL != 2.0 || a.Touched || a.MaxBreachPct != 0 {
		t.Fatalf("AAA = %+v", a)
	}
	b := byID["BBB"]
	// pnl = 3.0 − (90−88) = 1.0（miss 但权利金 > 击穿深度 → 正 P&L）
	if b.Outcome != "miss" || b.RealizedPnL != 1.0 || !b.Touched {
		t.Fatalf("BBB = %+v", b)
	}
	if b.MaxBreachPct < 5.5 || b.MaxBreachPct > 5.6 {
		t.Fatalf("BBB breach = %v, want ~5.56", b.MaxBreachPct)
	}
	if res.HitRate.Hit != 1 || res.HitRate.Miss != 1 || res.HitRate.Rate != 0.5 {
		t.Fatalf("hit_rate = %+v", res.HitRate)
	}
	// 幂等
	res2, _ := svc.Reconcile(context.Background())
	if res2.Settled != 0 {
		t.Fatalf("re-reconcile settled = %d, want 0", res2.Settled)
	}
}

func TestReconcileTouchedIncludesScanDate(t *testing.T) {
	fs := newFakeStore()
	// 只有 scan_date 当天 low < strike，之后回升 → 仍算 touched（闭区间含开仓日）
	seedCandidate(fs, "2026-07-20", "CCC", "2026-07-24", 100, 2.0)
	bars := map[string][]model.OHLCV{
		"CCC": {dailyBar("2026-07-20", 105, 98), dailyBar("2026-07-24", 110, 107)},
	}
	svc := reconcileService(fs, bars, "2026-07-27")
	res, _ := svc.Reconcile(context.Background())
	if res.Results[0].Outcome != "hit" || !res.Results[0].Touched {
		t.Fatalf("row = %+v, want hit+touched", res.Results[0])
	}
}

func TestReconcileGracePeriodPendingThenVoid(t *testing.T) {
	// expiry 2026-07-24，今天 7-30（到期后 6 天）无价 → pending；今天 8-01（8 天）→ void
	mk := func(today string) (*Service, *fakeStore) {
		fs := newFakeStore()
		seedCandidate(fs, "2026-07-20", "DDD", "2026-07-24", 100, 2.0)
		return reconcileService(fs, map[string][]model.OHLCV{}, today), fs
	}
	svc, fs := mk("2026-07-30")
	res, _ := svc.Reconcile(context.Background())
	if res.Pending != 1 || res.Settled != 0 || res.Void != 0 || len(fs.recs) != 0 {
		t.Fatalf("within grace: %+v recs=%d", res, len(fs.recs))
	}
	svc, fs = mk("2026-08-01")
	res, _ = svc.Reconcile(context.Background())
	if res.Void != 1 || res.Settled != 1 { // void 计入 settled 总数（写了 reconciliation）
		t.Fatalf("past grace: %+v", res)
	}
	rec := fs.recs[CandidateID("2026-07-20", "DDD", "2026-07-24", 100)]
	if rec.Outcome != "void" || rec.ExpiryClose != 0 || rec.RealizedPnL != 0 {
		t.Fatalf("void rec = %+v", rec)
	}
	if res.HitRate.Hit != 0 || res.HitRate.Miss != 0 || res.HitRate.Void != 1 {
		t.Fatalf("void must not enter hit/miss: %+v", res.HitRate)
	}
}

func TestReconcileExpiryBarMissingButLaterBarsExist(t *testing.T) {
	// 到期日 bar 缺失但已有到期之后的 bar → 数据源确定没有该日数据，
	// 仍在宽限期内 → pending（等数据源补齐或宽限期过后 void）
	fs := newFakeStore()
	seedCandidate(fs, "2026-07-20", "EEE", "2026-07-24", 100, 2.0)
	bars := map[string][]model.OHLCV{
		"EEE": {dailyBar("2026-07-20", 105, 102), dailyBar("2026-07-27", 108, 106)},
	}
	svc := reconcileService(fs, bars, "2026-07-28")
	res, _ := svc.Reconcile(context.Background())
	if res.Pending != 1 || res.Settled != 0 {
		t.Fatalf("res = %+v, want pending", res)
	}
}
