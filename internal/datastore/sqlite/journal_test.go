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
		SecType: "STK", Currency: "USD", Side: side, Shares: 100, Price: 50, AvgPrice: 50,
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

func TestExecutionCurrencyRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 16, 14, 0, 0, 0, time.UTC)
	if _, err := s.UpsertExecutions(ctx, []model.Execution{{
		ExecID: "E1", Time: now, Account: "DU1", Symbol: "0700", SecType: "STK",
		Currency: "HKD", Side: "BOT", Shares: 100, Price: 50, AvgPrice: 50,
	}}); err != nil {
		t.Fatalf("UpsertExecutions: %v", err)
	}

	got, err := s.ListExecutions(ctx, JournalFilter{})
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(got) != 1 || got[0].Currency != "HKD" {
		t.Fatalf("executions = %+v, want one HKD execution", got)
	}
}

func TestUpsertExecutionsBackfillsExplicitCurrencyForDuplicateExec(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 16, 14, 0, 0, 0, time.UTC)
	legacy := model.Execution{
		ExecID: "E1", Time: now, Account: "DU1", Symbol: "0700", SecType: "STK",
		Side: "BOT", Shares: 100, Price: 50, AvgPrice: 50,
	}
	if n, err := s.UpsertExecutions(ctx, []model.Execution{legacy}); err != nil || n != 1 {
		t.Fatalf("legacy insert: n=%d err=%v, want 1/nil", n, err)
	}

	corrected := legacy
	corrected.Currency = "HKD"
	if n, err := s.UpsertExecutions(ctx, []model.Execution{corrected}); err != nil || n != 0 {
		t.Fatalf("duplicate currency backfill: n=%d err=%v, want 0/nil", n, err)
	}

	got, err := s.ListExecutions(ctx, JournalFilter{})
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(got) != 1 || got[0].Currency != "HKD" {
		t.Fatalf("executions = %+v, want duplicate row corrected to HKD", got)
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

func TestSymbolBetaRoundTripAndFreshness(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	want := model.SymbolBeta{
		Symbol: "AAPL", Beta: 1.23, Observations: 60,
		AsOf: now.Add(-24 * time.Hour), UpdatedAt: now,
	}

	if err := s.UpsertSymbolBeta(ctx, want); err != nil {
		t.Fatalf("UpsertSymbolBeta: %v", err)
	}
	got, ok, err := s.GetFreshSymbolBeta(ctx, "aapl", 7*24*time.Hour, now.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("GetFreshSymbolBeta: %v", err)
	}
	if !ok {
		t.Fatal("expected fresh beta")
	}
	if got.Symbol != "AAPL" || got.Beta != want.Beta || got.Observations != want.Observations || !got.AsOf.Equal(want.AsOf) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	_, ok, err = s.GetFreshSymbolBeta(ctx, "AAPL", time.Hour, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("GetFreshSymbolBeta stale: %v", err)
	}
	if ok {
		t.Fatal("expected stale beta to miss")
	}
}

func TestSymbolBetaFreshnessRequiresRecentAsOf(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	err := s.UpsertSymbolBeta(ctx, model.SymbolBeta{
		Symbol: "AAPL", Beta: 1.23, Observations: 60,
		AsOf: now.Add(-30 * 24 * time.Hour), UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("UpsertSymbolBeta: %v", err)
	}

	_, ok, err := s.GetFreshSymbolBeta(ctx, "AAPL", 7*24*time.Hour, now)
	if err != nil {
		t.Fatalf("GetFreshSymbolBeta: %v", err)
	}
	if ok {
		t.Fatal("expected stale as_of to miss")
	}
}

func TestSymbolBetaFreshnessRejectsUnparseableTimestamps(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := s.UpsertSymbolBeta(ctx, model.SymbolBeta{
		Symbol: "AAPL", Beta: 1.23, Observations: 60,
		AsOf: now.Add(-24 * time.Hour), UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertSymbolBeta: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE symbol_beta SET updated_at = 'not-a-timestamp' WHERE symbol = 'AAPL'`); err != nil {
		t.Fatalf("corrupt timestamp: %v", err)
	}

	_, ok, err := s.GetFreshSymbolBeta(ctx, "AAPL", 7*24*time.Hour, now)
	if err != nil {
		t.Fatalf("GetFreshSymbolBeta: %v", err)
	}
	if ok {
		t.Fatal("expected unparseable timestamp to miss")
	}
}
