package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func TestIntelNarrativeInsertAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	n := model.IntelNarrative{
		EntryID: "e1", TradingDate: "2026-06-12", Checkpoint: "script",
		Phase: "premarket", Body: "今日剧本", CreatedAt: time.Now().UTC(),
	}
	if err := s.InsertIntelNarrative(ctx, n); err != nil {
		t.Fatal(err)
	}
	// append-only：同 entry_id 再插 → 唯一约束（INSERT OR IGNORE 静默跳过，不报错）
	if err := s.InsertIntelNarrative(ctx, n); err != nil {
		t.Fatalf("re-insert same entry_id should be ignored, got %v", err)
	}
	got, err := s.ListIntelNarratives(ctx, "2026-06-12")
	if err != nil || len(got) != 1 || got[0].Body != "今日剧本" {
		t.Fatalf("list = %+v err=%v", got, err)
	}
}

func TestIntelJudgmentInsertListAndExpired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(time.Hour)
	jPast := model.IntelJudgment{
		JudgmentID: "j1", TradingDate: "2026-06-12", Checkpoint: "first_check",
		AssetID: "SPX", AssetClass: "index", Direction: "up", ThresholdPct: 0.5,
		Confidence: 75, ExpiryCheckpoint: "reconcile", ExpiryAt: past,
		RegisteredPrice: 100, RegisteredBasis: "delayed", CreatedAt: time.Now().UTC(),
	}
	jFuture := jPast
	jFuture.JudgmentID = "j2"
	jFuture.ExpiryAt = future
	if err := s.InsertIntelJudgment(ctx, jPast); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertIntelJudgment(ctx, jFuture); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListIntelJudgments(ctx, "2026-06-12")
	if len(all) != 2 {
		t.Fatalf("want 2 judgments, got %d", len(all))
	}
	// 到期未结算：只有 j1（j2 未到期）
	exp, err := s.ExpiredUnsettledJudgments(ctx, time.Now().UTC())
	if err != nil || len(exp) != 1 || exp[0].JudgmentID != "j1" {
		t.Fatalf("expired = %+v err=%v", exp, err)
	}
	// 结算 j1 后，不再出现在 expired-unsettled
	if err := s.InsertIntelReconciliation(ctx, model.IntelReconciliation{
		JudgmentID: "j1", ExpiryPrice: 101, ExpiryBasis: "delayed",
		Outcome: "hit", DeltaPct: 1.0, SettledAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	exp2, _ := s.ExpiredUnsettledJudgments(ctx, time.Now().UTC())
	if len(exp2) != 0 {
		t.Fatalf("after settle, expired-unsettled should be empty, got %d", len(exp2))
	}
	recs, _ := s.ListIntelReconciliations(ctx, []string{"j1", "j2"})
	if recs["j1"].Outcome != "hit" {
		t.Errorf("recon j1 = %+v", recs["j1"])
	}
}

func TestPulseCloseNear(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 12, 20, 0, 0, 0, time.UTC) // 16:00 EDT
	_ = s.UpsertPulseBars(ctx, "SPX", []model.OHLCV{
		{Timestamp: base.Add(-10 * time.Minute), Close: 99},
		{Timestamp: base.Add(-5 * time.Minute), Close: 100},
	})
	// 目标 base：最近 ≤base 且在 30min 内的是 base-5min(close 100)
	px, _, ok, err := s.PulseCloseNear(ctx, "SPX", base, 30*time.Minute)
	if err != nil || !ok || px != 100 {
		t.Fatalf("near = %v ok=%v err=%v, want 100", px, ok, err)
	}
	// 容差外（base 早 2 小时，无 bar）→ ok=false
	_, _, ok2, _ := s.PulseCloseNear(ctx, "SPX", base.Add(-2*time.Hour), 30*time.Minute)
	if ok2 {
		t.Error("no bar within tolerance must return ok=false")
	}
}
