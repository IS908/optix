package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func pulseStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPulseBarsUpsertAndIncrementalRead(t *testing.T) {
	s := pulseStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	bars := []model.OHLCV{
		{Timestamp: t0, Close: 100}, {Timestamp: t0.Add(5 * time.Minute), Close: 101},
	}
	if err := s.UpsertPulseBars(ctx, "ES", bars); err != nil {
		t.Fatal(err)
	}
	// 重复 upsert 幂等
	if err := s.UpsertPulseBars(ctx, "ES", bars); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPulseBars(ctx, "ES", t0.Add(-time.Hour))
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%d err=%v", len(got), err)
	}
	last, err := s.LastPulseBarTS(ctx, "ES")
	if err != nil || !last.Equal(t0.Add(5*time.Minute)) {
		t.Fatalf("last=%v err=%v", last, err)
	}
	// 无数据资产 → 零值时间，不报错
	none, err := s.LastPulseBarTS(ctx, "NQ")
	if err != nil || !none.IsZero() {
		t.Fatalf("none=%v err=%v", none, err)
	}
}

func TestPulseBarsPruneKeeps2Days(t *testing.T) {
	s := pulseStore(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-72 * time.Hour)
	fresh := time.Now().UTC().Add(-1 * time.Hour)
	_ = s.UpsertPulseBars(ctx, "ES", []model.OHLCV{{Timestamp: old, Close: 1}, {Timestamp: fresh, Close: 2}})
	n, err := s.PrunePulseBars(ctx, 48*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("pruned=%d err=%v", n, err)
	}
	got, _ := s.GetPulseBars(ctx, "ES", time.Time{})
	if len(got) != 1 || got[0].Close != 2 {
		t.Fatalf("got=%+v", got)
	}
}
