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
