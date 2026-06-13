package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func TestGapStatsUpsertAndGet(t *testing.T) {
	s := newTestStore(t) // 既有 helper（journal_test.go）：临时文件库 + 全迁移
	ctx := context.Background()
	now := time.Now().UTC()
	stats := []model.PremarketGapStat{
		{Symbol: "SPX", Direction: "up", Band: "0.5-1", FillRate: 0.62, SampleN: 143, LookbackDays: 504, AsOf: now},
		{Symbol: "SPX", Direction: "down", Band: "0.5-1", FillRate: 0.55, SampleN: 120, LookbackDays: 504, AsOf: now},
	}
	if err := s.UpsertGapStats(ctx, stats); err != nil {
		t.Fatal(err)
	}
	// 重刷新（同 UNIQUE 键）→ REPLACE，不重复
	stats[0].FillRate = 0.70
	if err := s.UpsertGapStats(ctx, stats); err != nil {
		t.Fatal(err)
	}
	got, asOf, err := s.GetGapStats(ctx, "SPX")
	if err != nil || len(got) != 2 {
		t.Fatalf("get = %d rows err=%v", len(got), err)
	}
	if asOf.IsZero() {
		t.Error("as_of should be set")
	}
	var up model.PremarketGapStat
	for _, g := range got {
		if g.Direction == "up" {
			up = g
		}
	}
	if up.FillRate != 0.70 {
		t.Errorf("up fill_rate = %v, want 0.70 (replaced)", up.FillRate)
	}
}

func TestGetGapStatsEmpty(t *testing.T) {
	s := newTestStore(t)
	got, asOf, err := s.GetGapStats(context.Background(), "NOPE")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || !asOf.IsZero() {
		t.Errorf("empty symbol → 0 rows + zero as_of, got %d / %v", len(got), asOf)
	}
}
