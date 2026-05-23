package server

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/pkg/model"
)

type fakeAccountReader struct {
	execs []model.Execution
	err   error
}

func (f *fakeAccountReader) GetExecutions(ctx context.Context, _ model.ExecutionFilter) ([]model.Execution, error) {
	return f.execs, f.err
}

func (f *fakeAccountReader) GetPositions(ctx context.Context) ([]model.Position, error) {
	return nil, nil
}

func newJournalSvcForTest(t *testing.T, fake *fakeAccountReader) (*JournalService, *sqlite.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.New(filepath.Join(dir, "j.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return NewJournalService(fake, store), store
}

func TestSyncExecutionsHappyPath(t *testing.T) {
	fake := &fakeAccountReader{execs: []model.Execution{
		{ExecID: "E1", Time: time.Now(), Account: "DU1", Symbol: "AAPL",
			SecType: "STK", Side: "BOT", Shares: 100, Price: 50, AvgPrice: 50},
	}}
	svc, store := newJournalSvcForTest(t, fake)
	n, err := svc.SyncExecutions(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v, want 1/nil", n, err)
	}
	st, _ := store.GetSyncState(context.Background())
	if st.LastSyncAt.IsZero() || st.LastSyncCount != 1 || st.LastError != "" {
		t.Errorf("sync state = %+v", st)
	}
}

func TestSyncExecutionsErrorRecorded(t *testing.T) {
	fake := &fakeAccountReader{err: errors.New("ibkr down")}
	svc, store := newJournalSvcForTest(t, fake)
	if _, err := svc.SyncExecutions(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
	st, _ := store.GetSyncState(context.Background())
	if st.LastError == "" {
		t.Errorf("LastError should be set")
	}
}

// untilEOD mirrors the CLI/webui convention of treating `--until 2026-05-25`
// as "include trips through end-of-day 2026-05-25 UTC" (callers pre-adjust
// the date with `t.Add(24h - 1s)`). Tests use this so they exercise the
// same boundary semantics as production callers — otherwise tests that pass
// midnight Until values would silently disagree with how the CLI behaves.
func untilEOD(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 23, 59, 59, 0, time.UTC)
}

// R1: Open exec is before the review window, close (expiry) is inside.
// Current behavior filters execs by time first → matcher only sees the
// dangling close → emits a phantom "open" trip and misses realized P&L.
// Fix: load all execs, match, then filter trips by CloseTime.
func TestReviewIncludesTripsClosedInsideWindow(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAccountReader{}
	svc, store := newJournalSvcForTest(t, fake)

	openTime := time.Date(2026, 5, 15, 14, 0, 0, 0, time.UTC) // before window
	// 5/22 PUT @ 2.50 sold; expires worthless after 5/22 23:59:59 UTC.
	_, err := store.UpsertExecutions(ctx, []model.Execution{{
		ExecID: "E1", Time: openTime, Account: "DU1",
		Symbol: "GLW", SecType: "OPT",
		Expiration: "20260522", Strike: 185, Right: "P",
		Side: "SLD", Shares: 1, Price: 2.50, AvgPrice: 2.50,
		PermID: 1,
	}})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	since := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	until := untilEOD(2026, 5, 25)
	sum, err := svc.Review(ctx, since, until)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if sum.ExpiredRoundTrips != 1 {
		t.Errorf("ExpiredRoundTrips = %d, want 1", sum.ExpiredRoundTrips)
	}
	if sum.TotalRealizedPnL != 250 {
		t.Errorf("TotalRealizedPnL = %v, want +250", sum.TotalRealizedPnL)
	}
}

// R2: Both open and close inside the window — the simplest case, must still
// work after the refactor.
func TestReviewBothInsideWindow(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAccountReader{}
	svc, store := newJournalSvcForTest(t, fake)

	t0 := time.Date(2026, 5, 20, 14, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 5, 22, 14, 0, 0, 0, time.UTC)
	_, _ = store.UpsertExecutions(ctx, []model.Execution{
		{ExecID: "B1", Time: t0, Account: "DU1", Symbol: "AAPL", SecType: "STK",
			Side: "BOT", Shares: 100, AvgPrice: 150, PermID: 1},
		{ExecID: "S1", Time: t1, Account: "DU1", Symbol: "AAPL", SecType: "STK",
			Side: "SLD", Shares: 100, AvgPrice: 160, PermID: 2},
	})

	sum, err := svc.Review(ctx,
		time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		untilEOD(2026, 5, 25))
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if sum.ClosedRoundTrips != 1 || sum.TotalRealizedPnL != 1000 {
		t.Errorf("sum = %+v, want 1 closed @ +1000", sum)
	}
	// Lock in the TotalExecutions contract: counts raw fills with execution
	// time in the window, NOT trips. Both execs (5/20 BOT, 5/22 SLD) fall in
	// [5/19, 5/25] → 2. This is intentionally a different metric than
	// TotalRoundTrips (1) — see Review's comment on the divergence.
	if sum.TotalExecutions != 2 {
		t.Errorf("TotalExecutions = %d, want 2 (raw exec count in window)", sum.TotalExecutions)
	}
}

// R3: Open inside window but trip still open → not counted in realized review.
func TestReviewSkipsOpenTrips(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAccountReader{}
	svc, store := newJournalSvcForTest(t, fake)

	t0 := time.Date(2026, 5, 20, 14, 0, 0, 0, time.UTC)
	_, _ = store.UpsertExecutions(ctx, []model.Execution{
		{ExecID: "B1", Time: t0, Account: "DU1", Symbol: "AAPL", SecType: "STK",
			Side: "BOT", Shares: 100, AvgPrice: 150, PermID: 1},
	})

	sum, err := svc.Review(ctx,
		time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		untilEOD(2026, 5, 25))
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if sum.ClosedRoundTrips != 0 || sum.ExpiredRoundTrips != 0 || sum.TotalRealizedPnL != 0 {
		t.Errorf("sum = %+v, want all zeros (only open trip exists)", sum)
	}
	// Lock in the "windowed query excludes open trips entirely" contract:
	// open trips don't even count toward OpenRoundTrips when a window is set.
	if sum.OpenRoundTrips != 0 {
		t.Errorf("OpenRoundTrips = %d, want 0 (windowed query excludes open trips)", sum.OpenRoundTrips)
	}
}

// R4: Trip closes exactly on the until boundary. With Until set to EOD of
// 5/22 (5/22 23:59:59 UTC — mirroring the CLI convention) and CloseTime
// also at 5/22 23:59:59, the inclusive `!CloseTime.After(Until)` check
// includes the trip. Uses a past expiration so the test is wall-clock-
// independent.
func TestReviewBoundaryInclusive(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAccountReader{}
	svc, store := newJournalSvcForTest(t, fake)

	openTime := time.Date(2026, 5, 20, 14, 0, 0, 0, time.UTC)
	_, _ = store.UpsertExecutions(ctx, []model.Execution{{
		ExecID: "E1", Time: openTime, Account: "DU1",
		Symbol: "GLW", SecType: "OPT",
		Expiration: "20260522", Strike: 185, Right: "P",
		Side: "SLD", Shares: 1, AvgPrice: 1.00, PermID: 1,
	}})

	// Until = 5/22 EOD inclusive (matches CLI's `t.Add(24h - 1s)` adjustment).
	// Trip CloseTime is 5/22 23:59:59. `CloseTime.After(Until)` is false →
	// included.
	sum, err := svc.Review(ctx,
		time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		untilEOD(2026, 5, 22))
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if sum.ExpiredRoundTrips != 1 {
		t.Errorf("boundary trip not included: sum = %+v", sum)
	}
}

// R6: Regression for the off-by-one overshoot. With Until at EOD of 5/22
// (the CLI convention), a trip whose CloseTime is on 5/23 MUST be excluded.
// Earlier versions of this code added an extra day on top of the CLI's
// already-EOD adjustment, silently widening user windows by ~24h.
func TestReviewDoesNotOvershootUntilBoundary(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAccountReader{}
	svc, store := newJournalSvcForTest(t, fake)

	openTime := time.Date(2026, 5, 20, 14, 0, 0, 0, time.UTC)
	_, _ = store.UpsertExecutions(ctx, []model.Execution{{
		ExecID: "E1", Time: openTime, Account: "DU1",
		Symbol: "GLW", SecType: "OPT",
		Expiration: "20260523", Strike: 185, Right: "P", // expires 5/23 (one day past Until)
		Side: "SLD", Shares: 1, AvgPrice: 1.00, PermID: 1,
	}})

	sum, err := svc.Review(ctx,
		time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		untilEOD(2026, 5, 22)) // user asked through end-of-day 5/22
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if sum.ExpiredRoundTrips != 0 || sum.TotalRealizedPnL != 0 {
		t.Errorf("trip closing on 5/23 leaked into Until=5/22 EOD window: sum = %+v", sum)
	}
}

// R5: Multi-window isolation — a trip closed in an earlier week must not
// leak into the current week's Review.
func TestReviewMultiWindowIsolation(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAccountReader{}
	svc, store := newJournalSvcForTest(t, fake)

	openA := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	closeA := time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	openB := time.Date(2026, 5, 15, 14, 0, 0, 0, time.UTC)
	// Trip A: stock closed 5/8 (outside current window)
	// Trip B: option expired 5/22 (inside current window)
	_, _ = store.UpsertExecutions(ctx, []model.Execution{
		{ExecID: "A-O", Time: openA, Account: "DU1", Symbol: "AAPL", SecType: "STK",
			Side: "BOT", Shares: 100, AvgPrice: 150, PermID: 1},
		{ExecID: "A-C", Time: closeA, Account: "DU1", Symbol: "AAPL", SecType: "STK",
			Side: "SLD", Shares: 100, AvgPrice: 160, PermID: 2},
		{ExecID: "B-O", Time: openB, Account: "DU1", Symbol: "GLW", SecType: "OPT",
			Expiration: "20260522", Strike: 185, Right: "P",
			Side: "SLD", Shares: 1, AvgPrice: 2.50, PermID: 3},
	})

	sum, err := svc.Review(ctx,
		time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		untilEOD(2026, 5, 25))
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	// Only trip B (+250) counted; trip A (+1000) is outside the window.
	if sum.TotalRealizedPnL != 250 {
		t.Errorf("realized = %v, want +250 (trip A excluded)", sum.TotalRealizedPnL)
	}
	if sum.ClosedRoundTrips+sum.ExpiredRoundTrips != 1 {
		t.Errorf("closed+expired = %d, want 1", sum.ClosedRoundTrips+sum.ExpiredRoundTrips)
	}
}
