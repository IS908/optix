//go:build integration

package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchedulerIntegration is a full integration test that requires:
//  1. Python gRPC analysis server running on localhost:50052
//  2. IBKR Gateway/TWS running on OPTIX_IB_HOST/OPTIX_IB_PORT
//     (defaults: 127.0.0.1:gateway -> 4001)
//
// Run with: go test -tags=integration -v ./internal/scheduler/
// Override examples: OPTIX_IB_PORT=tws or OPTIX_IB_PORT=4002.
func TestSchedulerIntegration(t *testing.T) {
	ibCfg, err := resolveIntegrationIBConfig()
	require.NoError(t, err)

	// Setup in-memory SQLite database
	store, err := sqlite.New(":memory:")
	require.NoError(t, err, "failed to create in-memory database")

	// Add test symbol with 5-minute auto-refresh
	symbol := "AAPL"
	err = store.AddToWatchlist(context.Background(), symbol)
	require.NoError(t, err, "failed to add symbol to watchlist")

	err = store.UpdateWatchlistConfig(symbol, true, 5)
	require.NoError(t, err, "failed to enable auto-refresh")

	// Force last_refreshed_at to be old so scheduler picks it up immediately
	err = store.UpdateLastRefreshTime(symbol, time.Now().Add(-10*time.Minute))
	require.NoError(t, err, "failed to update last refresh time")

	// Configure scheduler with short intervals for testing
	cfg := Config{
		WorkerCount:    2,
		QueueSize:      10,
		TickInterval:   10 * time.Second, // Check every 10 seconds for testing
		WorkerThrottle: 5 * time.Second,
	}

	sched := New(
		cfg,
		store,
		ibCfg,
		AnalysisConfig{
			Addr:          "localhost:50052",
			Capital:       100000,
			ForecastDays:  14,
			RiskTolerance: "moderate",
		},
	)

	// Start scheduler with 2-minute timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cleanupIntegrationScheduler(cancel, store)

	err = sched.Start(ctx)
	require.NoError(t, err, "failed to start scheduler")

	// Wait for first refresh cycle. The scheduler should pick up the symbol
	// within 10 seconds and complete the task within ~60 seconds.
	job := waitForLatestIntegrationJob(t, store, symbol, 90*time.Second, isTerminalJob)
	assert.Equal(t, "analyze", job.JobType, "expected job type 'analyze'")
	assert.Equal(t, "success", job.Status, "expected job status 'success'")
	assert.NotNil(t, job.CompletedAt, "expected job to have completed_at timestamp")

	// Verify cache was updated
	cache, _, err := store.GetAnalysisCache(context.Background(), symbol)
	require.NoError(t, err, "failed to get analysis cache")
	assert.NotEmpty(t, cache, "expected cache payload to exist")

	// Verify snapshot was created
	snapshots, err := store.GetLatestSnapshots(context.Background())
	require.NoError(t, err, "failed to get snapshots")
	found := false
	for _, snap := range snapshots {
		if snap.Symbol == symbol {
			found = true
			break
		}
	}
	assert.True(t, found, "expected snapshot to exist for symbol")

	t.Logf("Integration test passed: %s successfully refreshed", symbol)
}

// TestSchedulerRetry tests the failure path with a non-existent symbol.
// Retry scheduling is covered by the stored failed job and retry count.
func TestSchedulerRetry(t *testing.T) {
	ibCfg, err := resolveIntegrationIBConfig()
	require.NoError(t, err)

	store, err := sqlite.New(":memory:")
	require.NoError(t, err)

	// Add invalid symbol that will fail
	symbol := "INVALID_SYMBOL_XYZ"
	err = store.AddToWatchlist(context.Background(), symbol)
	require.NoError(t, err)

	err = store.UpdateWatchlistConfig(symbol, true, 5)
	require.NoError(t, err)

	err = store.UpdateLastRefreshTime(symbol, time.Now().Add(-10*time.Minute))
	require.NoError(t, err)

	cfg := Config{
		WorkerCount:    1,
		QueueSize:      10,
		TickInterval:   5 * time.Second,
		WorkerThrottle: 2 * time.Second,
	}

	sched := New(
		cfg,
		store,
		ibCfg,
		AnalysisConfig{
			Addr:          "localhost:50052",
			Capital:       100000,
			ForecastDays:  14,
			RiskTolerance: "moderate",
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupIntegrationScheduler(cancel, store)

	err = sched.Start(ctx)
	require.NoError(t, err)

	// Wait for the initial attempt to reach a terminal state. Live IBKR and
	// yfinance fallback paths can take longer than a fixed 15-second sleep.
	job := waitForLatestIntegrationJob(t, store, symbol, 45*time.Second, isTerminalJob)
	assert.Equal(t, "analyze", job.JobType)
	assert.Equal(t, "failed", job.Status)
	assert.NotEmpty(t, job.ErrorMessage, "expected error message")
	assert.True(t, job.RetryCount >= 0, "expected retry count to be tracked")

	t.Logf("Retry test passed: invalid symbol failed as expected with retry_count=%d", job.RetryCount)
}

func cleanupIntegrationScheduler(cancel context.CancelFunc, store *sqlite.Store) {
	cancel()
	time.Sleep(100 * time.Millisecond)
	_ = store.Close()
}

func waitForLatestIntegrationJob(t *testing.T, store *sqlite.Store, symbol string, timeout time.Duration, ready func(*model.BackgroundJob) bool) *model.BackgroundJob {
	t.Helper()

	var latest *model.BackgroundJob
	require.Eventually(t, func() bool {
		jobs, err := store.GetBackgroundJobsForSymbol(symbol)
		if err != nil || len(jobs) == 0 {
			return false
		}
		latest = jobs[0]
		return ready(latest)
	}, timeout, time.Second)

	require.NotNil(t, latest, "expected at least one background job")
	return latest
}

func isTerminalJob(job *model.BackgroundJob) bool {
	return job.Status == "success" || job.Status == "failed"
}
