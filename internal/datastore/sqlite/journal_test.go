package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleExec(execID, symbol, side string, ts time.Time) model.Execution {
	return model.Execution{
		ExecID: execID, Time: ts, Account: "DU1", Symbol: symbol,
		SecType: "STK", Side: side, Shares: 100, Price: 50, AvgPrice: 50,
		Exchange: "SMART", OrderID: 42, PermID: 7,
	}
}

func TestUpsertExecutionsInsertsAndDedups(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 16, 14, 0, 0, 0, time.UTC)
	execs := []model.Execution{
		sampleExec("E1", "AAPL", "BOT", now),
		sampleExec("E2", "AAPL", "SLD", now.Add(time.Hour)),
	}

	n, err := s.UpsertExecutions(ctx, execs)
	if err != nil || n != 2 {
		t.Fatalf("first run: n=%d err=%v, want 2/nil", n, err)
	}

	n, err = s.UpsertExecutions(ctx, execs)
	if err != nil || n != 0 {
		t.Fatalf("second run: n=%d err=%v, want 0/nil", n, err)
	}

	mixed := []model.Execution{execs[0], sampleExec("E3", "AAPL", "BOT", now.Add(2*time.Hour))}
	n, err = s.UpsertExecutions(ctx, mixed)
	if err != nil || n != 1 {
		t.Fatalf("mixed run: n=%d err=%v, want 1/nil", n, err)
	}
}

func TestListExecutionsFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)
	execs := []model.Execution{
		sampleExec("E1", "AAPL", "BOT", base),
		sampleExec("E2", "AAPL", "SLD", base.Add(24*time.Hour)),
		sampleExec("E3", "TSLA", "BOT", base.Add(48*time.Hour)),
	}
	if _, err := s.UpsertExecutions(ctx, execs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cases := []struct {
		name   string
		filter JournalFilter
		want   int
	}{
		{"no filter", JournalFilter{}, 3},
		{"by symbol", JournalFilter{Symbol: "AAPL"}, 2},
		{"by side", JournalFilter{Side: "BOT"}, 2},
		{"by since", JournalFilter{Since: base.Add(36 * time.Hour)}, 1},
		{"by until", JournalFilter{Until: base.Add(36 * time.Hour)}, 2},
		{"with limit", JournalFilter{Limit: 1}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ListExecutions(ctx, tc.filter)
			if err != nil {
				t.Fatalf("ListExecutions: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("len=%d, want %d", len(got), tc.want)
			}
		})
	}
}

func TestSyncStateRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	st, err := s.GetSyncState(ctx)
	if err != nil {
		t.Fatalf("GetSyncState: %v", err)
	}
	if !st.LastSyncAt.IsZero() {
		t.Errorf("initial last_sync_at = %v, want zero", st.LastSyncAt)
	}

	now := time.Date(2026, 5, 16, 14, 0, 0, 0, time.UTC)
	want := SyncState{LastSyncAt: now, LastSyncCount: 3}
	if err := s.UpdateSyncState(ctx, want); err != nil {
		t.Fatalf("UpdateSyncState: %v", err)
	}
	got, err := s.GetSyncState(ctx)
	if err != nil {
		t.Fatalf("GetSyncState (2): %v", err)
	}
	if !got.LastSyncAt.Equal(want.LastSyncAt) || got.LastSyncCount != want.LastSyncCount {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
