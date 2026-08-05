package scanjournal

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func settled(fs *fakeStore, scanDate, symbol string, score float64, outcome string, pnl float64, touched bool) {
	c := sampleModelCandidate(scanDate, symbol, "2026-07-24", 100, 2.0)
	c.Score = score
	fs.cands = append(fs.cands, c)
	fs.recs[c.CandidateID] = model.ScanReconciliation{
		CandidateID: c.CandidateID, Outcome: outcome, RealizedPnL: pnl,
		Touched: touched, ExpiryBasis: "delayed", SettledAt: time.Now().UTC(),
	}
}

func settledWith(fs *fakeStore, symbol string, rank, dte int, outcome string) {
	c := sampleModelCandidate("2026-07-20", symbol, "2026-07-24", 100, 2.0)
	c.Rank = rank
	c.DTE = dte
	c.CandidateID = c.CandidateID + symbol // 防 UNIQUE 锚点碰撞（同价同到期不同 rank）
	fs.cands = append(fs.cands, c)
	fs.recs[c.CandidateID] = model.ScanReconciliation{
		CandidateID: c.CandidateID, Outcome: outcome, RealizedPnL: 1,
		ExpiryBasis: "delayed", SettledAt: time.Now().UTC(),
	}
}

func TestStatsScoreBandsTerciles(t *testing.T) {
	fs := newFakeStore()
	// 6 条结算：score 6/5(高) 4/3(中) 2/1(低)；高档全 hit，低档全 miss
	settled(fs, "2026-07-20", "A1", 6, "hit", 2.0, false)
	settled(fs, "2026-07-20", "A2", 5, "hit", 1.5, false)
	settled(fs, "2026-07-20", "B1", 4, "hit", 1.0, true)
	settled(fs, "2026-07-20", "B2", 3, "miss", -0.5, true)
	settled(fs, "2026-07-20", "C1", 2, "miss", -1.0, true)
	settled(fs, "2026-07-20", "C2", 1, "miss", -2.0, true)
	svc := newTestService(fs)
	res, err := svc.Stats(context.Background(), "all", "score-band")
	if err != nil || len(res.Bands) != 3 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if res.Bands[0].Label != "high" || res.Bands[0].HitRate != 1.0 || res.Bands[0].N != 2 {
		t.Fatalf("high band = %+v", res.Bands[0])
	}
	if res.Bands[2].Label != "low" || res.Bands[2].HitRate != 0.0 || res.Bands[2].TouchedRate != 1.0 {
		t.Fatalf("low band = %+v", res.Bands[2])
	}
}

func TestStatsDegradesBelowThreeSettled(t *testing.T) {
	fs := newFakeStore()
	settled(fs, "2026-07-20", "A1", 6, "hit", 2.0, false)
	settled(fs, "2026-07-20", "A2", 5, "miss", -1.0, true)
	svc := newTestService(fs)
	res, err := svc.Stats(context.Background(), "all", "score-band")
	if err != nil || len(res.Bands) != 1 || res.Bands[0].Label != "all" || res.Note == "" {
		t.Fatalf("res=%+v err=%v, want single 'all' band + note", res, err)
	}
}

func TestStatsWindowFiltersAndVoidExcluded(t *testing.T) {
	fs := newFakeStore()
	settled(fs, "2026-05-01", "OLD", 6, "hit", 2.0, false)
	settled(fs, "2026-07-20", "NEW", 5, "miss", -1.0, false)
	settled(fs, "2026-07-21", "VOIDED", 4, "void", 0, false)
	svc := newTestService(fs) // Now = 2026-07-29
	res, err := svc.Stats(context.Background(), "30d", "score-band")
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, b := range res.Bands {
		total += b.N
	}
	if total != 1 { // OLD 在窗口外，VOIDED 不计
		t.Fatalf("total settled in 30d window = %d, want 1 (bands=%+v)", total, res.Bands)
	}
}

// 数字序而非字典序：rank-2 必须在 rank-10 之前。
func TestStatsByRankNumericOrder(t *testing.T) {
	fs := newFakeStore()
	settledWith(fs, "A", 10, 9, "hit")
	settledWith(fs, "B", 2, 9, "miss")
	settledWith(fs, "C", 1, 9, "hit")
	svc := newTestService(fs)
	res, err := svc.Stats(context.Background(), "all", "rank")
	if err != nil {
		t.Fatal(err)
	}
	labels := []string{}
	for _, b := range res.Bands {
		labels = append(labels, b.Label)
	}
	want := []string{"rank-1", "rank-2", "rank-10"}
	if len(labels) != 3 || labels[0] != want[0] || labels[1] != want[1] || labels[2] != want[2] {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
}

// dte 档自然升序：7-10 在前，18-24 在后。
func TestStatsByDTENaturalOrder(t *testing.T) {
	fs := newFakeStore()
	settledWith(fs, "A", 1, 22, "hit")
	settledWith(fs, "B", 2, 8, "miss")
	settledWith(fs, "C", 3, 14, "hit")
	svc := newTestService(fs)
	res, err := svc.Stats(context.Background(), "all", "dte")
	if err != nil {
		t.Fatal(err)
	}
	labels := []string{}
	for _, b := range res.Bands {
		labels = append(labels, b.Label)
	}
	want := []string{"dte-7-10", "dte-11-17", "dte-18-24"}
	if len(labels) != 3 || labels[0] != want[0] || labels[1] != want[1] || labels[2] != want[2] {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
}
