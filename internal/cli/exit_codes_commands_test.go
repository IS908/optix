package cli

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/spf13/cobra"
)

func TestSQLiteOpenFailuresUseDocumentedExitCode(t *testing.T) {
	tests := []struct {
		name string
		cmd  func() *cobra.Command
		args []string
	}{
		{name: "quote", cmd: newQuoteCmd, args: []string{"AAPL"}},
		{name: "analyze", cmd: newAnalyzeCmd, args: []string{"AAPL"}},
		{name: "dashboard", cmd: newDashboardCmd},
		{name: "positions", cmd: newPositionsCmd},
		{name: "trades", cmd: newTradesCmd},
		{name: "watch add", cmd: newWatchAddCmd, args: []string{"AAPL"}},
		{name: "watch remove", cmd: newWatchRemoveCmd, args: []string{"AAPL"}},
		{name: "watch list", cmd: newWatchListCmd},
		{name: "server", cmd: newServerCmd},
		{name: "max-pain", cmd: newMaxPainCmd, args: []string{"AAPL", "--source", "yfinance"}},
		{name: "pulse", cmd: newPulseCmd},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldDBPath := dbPath
			t.Cleanup(func() { dbPath = oldDBPath })

			// Passing a directory as the database path makes SQLite fail during
			// migration before any broker or analysis-engine connection is tried.
			dbPath = filepath.Dir(t.TempDir())

			cmd := tc.cmd()
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected sqlite open failure")
			}
			if got := AsExitCode(err); got != exitSQLiteErr {
				t.Fatalf("AsExitCode(err) = %d, want %d; err=%v", got, exitSQLiteErr, err)
			}
		})
	}
}

func TestMaxPainBrokerFailuresUseDocumentedExitCode(t *testing.T) {
	oldPythonBin := pythonBin
	oldDBPath := dbPath
	t.Cleanup(func() {
		pythonBin = oldPythonBin
		dbPath = oldDBPath
	})
	pythonBin = filepath.Join(t.TempDir(), "missing-python")
	dbPath = filepath.Join(t.TempDir(), "optix.db")

	cmd := newMaxPainCmd()
	cmd.SetArgs([]string{"AAPL", "--source", "yfinance"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected broker failure")
	}
	if got := AsExitCode(err); got != exitIBKRUnreachable {
		t.Fatalf("AsExitCode(err) = %d, want %d; err=%v", got, exitIBKRUnreachable, err)
	}
}

func TestMaxPainInvalidFormatUsesGenericExitCode(t *testing.T) {
	cmd := newMaxPainCmd()
	cmd.SetArgs([]string{"AAPL", "--format", "xml"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected invalid format error")
	}
	if got := AsExitCode(err); got != exitGenericErr {
		t.Fatalf("AsExitCode(err) = %d, want %d; err=%v", got, exitGenericErr, err)
	}
}

func TestWatchlistReadFailuresUseSQLiteExitCode(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "analyze watchlist",
			run: func() error {
				return runWatchlistAnalysis(context.Background(), 14, 50000, "moderate", "localhost:0", "text")
			},
		},
		{
			name: "dashboard",
			run: func() error {
				cmd := newDashboardCmd()
				return cmd.Execute()
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldDBPath := dbPath
			t.Cleanup(func() { dbPath = oldDBPath })
			dbPath = makeWatchlistScanFailureDB(t)

			err := tc.run()
			if err == nil {
				t.Fatal("expected watchlist read failure")
			}
			if got := AsExitCode(err); got != exitSQLiteErr {
				t.Fatalf("AsExitCode(err) = %d, want %d; err=%v", got, exitSQLiteErr, err)
			}
		})
	}
}

func TestWatchlistAnalysisAllFailuresUseDocumentedExitCode(t *testing.T) {
	tests := []struct {
		name            string
		successes       int
		firstFetchErr   error
		firstAnalyzeErr error
		want            int
	}{
		{
			name:          "all fetch failures",
			firstFetchErr: sql.ErrConnDone,
			want:          exitIBKRUnreachable,
		},
		{
			name:            "all analysis failures",
			firstAnalyzeErr: sql.ErrNoRows,
			want:            exitGenericErr,
		},
		{
			name:            "mixed fetch and analysis failures",
			firstFetchErr:   sql.ErrConnDone,
			firstAnalyzeErr: sql.ErrNoRows,
			want:            exitGenericErr,
		},
		{
			name:          "some success ignores failures",
			successes:     1,
			firstFetchErr: sql.ErrConnDone,
			want:          exitOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := watchlistAnalysisExit(tc.successes, tc.firstFetchErr, tc.firstAnalyzeErr)
			if got := AsExitCode(err); got != tc.want {
				t.Fatalf("AsExitCode(err) = %d, want %d; err=%v", got, tc.want, err)
			}
		})
	}
}

func TestWatchRemoveCascadeFailuresUseSQLiteExitCode(t *testing.T) {
	oldDBPath := dbPath
	t.Cleanup(func() { dbPath = oldDBPath })
	dbPath = makeWatchRemoveCascadeFailureDB(t)

	cmd := newWatchRemoveCmd()
	cmd.SetArgs([]string{"AAPL"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected cascade delete failure")
	}
	if got := AsExitCode(err); got != exitSQLiteErr {
		t.Fatalf("AsExitCode(err) = %d, want %d; err=%v", got, exitSQLiteErr, err)
	}
}

func makeWatchRemoveCascadeFailureDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "optix.db")
	store, err := sqlite.New(path)
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	if err := store.AddToWatchlist(context.Background(), "AAPL"); err != nil {
		t.Fatalf("seed watchlist: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
		DROP TABLE watchlist_snapshots;
		CREATE VIEW watchlist_snapshots AS SELECT 'AAPL' AS symbol;
	`)
	if err != nil {
		t.Fatalf("replace watchlist_snapshots with view: %v", err)
	}
	return path
}

func makeWatchlistScanFailureDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "optix.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE watchlist (
			symbol TEXT PRIMARY KEY,
			added_at TEXT NOT NULL,
			notes TEXT DEFAULT '',
			tags TEXT DEFAULT '[]',
			auto_refresh_enabled TEXT,
			refresh_interval_minutes TEXT
		);
		INSERT INTO watchlist (symbol, added_at, auto_refresh_enabled, refresh_interval_minutes)
		VALUES ('AAPL', '2026-06-01T00:00:00Z', 'not-an-int', '15');
	`)
	if err != nil {
		t.Fatalf("seed sqlite: %v", err)
	}
	return path
}
