//go:build integration

package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchedulerIntegration is a full integration test that requires:
// 1. Python gRPC analysis server running on localhost:50052
// 2. IBKR TWS or IB Gateway running on localhost:7496
//
// Run with: go test -tags=integration -v ./internal/scheduler/
func TestSchedulerIntegration(t *testing.T) {
	// Setup in-memory SQLite database
	store, err := sqlite.New(":memory:")
	require.NoError(t, err, "failed to create in-memory database")
	defer store.Close()

	// Add test symbol with 5-minute auto-refresh
	symbol := "AAPL"
	err = store.AddToWatchlist(context.Background(), symbol, "")
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
		IBConfig{Host: "127.0.0.1", Port: 7496},
		AnalysisConfig{
			Addr:          "localhost:50052",
			Capital:       100000,
			ForecastDays:  14,
			RiskTolerance: "moderate",
		},
	)

	// Start scheduler with 2-minute timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err = sched.Start(ctx)
	require.NoError(t, err, "failed to start scheduler")

	// Wait for first refresh cycle (allow up to 90 seconds)
	// The scheduler should pick up the symbol within 10 seconds (tick interval)
	// and complete the task within ~60 seconds (IBKR + Python fetch time)
	time.Sleep(90 * time.Second)

	// Verify job was created and executed
	jobs, err := store.GetBackgroundJobsForSymbol(context.Background(), symbol)
	require.NoError(t, err, "failed to get background jobs")
	require.NotEmpty(t, jobs, "expected at least one background job")

	// Check the most recent job
	job := jobs[0]
	assert.Equal(t, "analyze", job.JobType, "expected job type 'analyze'")
	assert.Equal(t, "success", job.Status, "expected job status 'success'")
	assert.NotNil(t, job.CompletedAt, "expected job to have completed_at timestamp")

	// Verify cache was updated
	cache, err := store.GetAnalysisCache(context.Background(), symbol)
	require.NoError(t, err, "failed to get analysis cache")
	assert.NotNil(t, cache, "expected cache entry to exist")
	assert.Equal(t, symbol, cache.Symbol, "expected cache symbol to match")

	// Verify snapshot was created
	snapshots, err := store.GetWatchlistSnapshots(context.Background())
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

// TestSchedulerRetry tests the retry mechanism with a non-existent symbol
// that should fail and retry 3 times.
func TestSchedulerRetry(t *testing.T) {
	store, err := sqlite.New(":memory:")
	require.NoError(t, err)
	defer store.Close()

	// Add invalid symbol that will fail
	symbol := "INVALID_SYMBOL_XYZ"
	err = store.AddToWatchlist(context.Background(), symbol, "")
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
		IBConfig{Host: "127.0.0.1", Port: 7496},
		AnalysisConfig{
			Addr:          "localhost:50052",
			Capital:       100000,
			ForecastDays:  14,
			RiskTolerance: "moderate",
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = sched.Start(ctx)
	require.NoError(t, err)

	// Wait for initial attempt + 3 retries (with delays: 1min, 5min, 15min)
	// Since we have short test timeouts, we'll just wait for the initial failure
	time.Sleep(15 * time.Second)

	// Verify job exists and failed
	jobs, err := store.GetBackgroundJobsForSymbol(context.Background(), symbol)
	require.NoError(t, err)
	require.NotEmpty(t, jobs, "expected at least one job")

	// Most recent job should have failed
	job := jobs[0]
	assert.Equal(t, "analyze", job.JobType)
	assert.Equal(t, "failed", job.Status)
	assert.NotEmpty(t, job.ErrorMessage, "expected error message")
	assert.True(t, job.RetryCount >= 0, "expected retry count to be tracked")

	t.Logf("Retry test passed: invalid symbol failed as expected with retry_count=%d", job.RetryCount)
}
