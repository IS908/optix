package scheduler

import (
	"math"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// groupByInterval groups symbols by their refresh interval.
func groupByInterval(symbols []model.SymbolRefresh) map[int][]string {
	groups := make(map[int][]string)
	for _, s := range symbols {
		groups[s.Interval] = append(groups[s.Interval], s.Symbol)
	}
	return groups
}

// shouldDispatchBatch determines if a batch should be dispatched now based on
// interval timing and symbol count. This implements the hybrid batching strategy:
// distribute batches evenly over the interval window.
//
// Example: 15min interval with 10 stocks → 3 batches spread 5min apart
func shouldDispatchBatch(interval int, lastBatch, now time.Time, symbolCount int) bool {
	if lastBatch.IsZero() {
		return true // First batch always dispatched
	}

	elapsed := now.Sub(lastBatch).Minutes()

	// Calculate batch interval: distribute batches evenly over the refresh interval
	// E.g., 15min interval with 10 symbols (2 batches of 5) → batch every 7.5 minutes
	batchesNeeded := math.Ceil(float64(symbolCount) / 5.0) // Max 5 symbols per batch
	batchInterval := float64(interval) / batchesNeeded

	return elapsed >= batchInterval
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
