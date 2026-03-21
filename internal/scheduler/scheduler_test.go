package scheduler

import (
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func TestBatchGeneration(t *testing.T) {
	symbols := []model.SymbolRefresh{
		{Symbol: "AAPL", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
		{Symbol: "TSLA", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
		{Symbol: "NVDA", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
		{Symbol: "MSFT", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
		{Symbol: "GOOGL", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
		{Symbol: "AMZN", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
		{Symbol: "META", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
		{Symbol: "NFLX", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
		{Symbol: "AMD", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
		{Symbol: "INTC", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
	}

	grouped := groupByInterval(symbols)

	if len(grouped) != 1 {
		t.Errorf("Expected 1 group, got %d", len(grouped))
	}

	if len(grouped[15]) != 10 {
		t.Errorf("Expected 10 symbols in 15-minute group, got %d", len(grouped[15]))
	}

	// Test batch size limiting
	batch := grouped[15][:min(5, len(grouped[15]))]
	if len(batch) != 5 {
		t.Errorf("Expected batch size of 5, got %d", len(batch))
	}
}

func TestRetryDelayCalculation(t *testing.T) {
	tests := []struct {
		retryCount int
		expected   time.Duration
	}{
		{1, 1 * time.Minute},
		{2, 5 * time.Minute},
		{3, 15 * time.Minute},
		{4, 15 * time.Minute}, // Max out at 15 minutes
	}

	for _, tt := range tests {
		actual := calculateRetryDelay(tt.retryCount)
		if actual != tt.expected {
			t.Errorf("calculateRetryDelay(%d) = %v, expected %v", tt.retryCount, actual, tt.expected)
		}
	}
}

func TestShouldDispatchBatch(t *testing.T) {
	now := time.Now()

	// First batch should always dispatch
	if !shouldDispatchBatch(15, time.Time{}, now, 10) {
		t.Error("First batch should always dispatch")
	}

	// Within interval window - should not dispatch
	lastBatch := now.Add(-4 * time.Minute)
	if shouldDispatchBatch(15, lastBatch, now, 10) {
		t.Error("Should not dispatch within interval window")
	}

	// After interval window - should dispatch
	lastBatch = now.Add(-8 * time.Minute)
	if !shouldDispatchBatch(15, lastBatch, now, 10) {
		t.Error("Should dispatch after interval window")
	}

	// Edge case: very recent batch
	lastBatch = now.Add(-30 * time.Second)
	if shouldDispatchBatch(15, lastBatch, now, 10) {
		t.Error("Should not dispatch so soon after last batch")
	}
}

func TestGroupByInterval(t *testing.T) {
	symbols := []model.SymbolRefresh{
		{Symbol: "AAPL", Interval: 5},
		{Symbol: "TSLA", Interval: 15},
		{Symbol: "NVDA", Interval: 15},
		{Symbol: "MSFT", Interval: 30},
	}

	grouped := groupByInterval(symbols)

	if len(grouped) != 3 {
		t.Errorf("Expected 3 groups, got %d", len(grouped))
	}

	if len(grouped[5]) != 1 {
		t.Errorf("Expected 1 symbol in 5-minute group, got %d", len(grouped[5]))
	}

	if len(grouped[15]) != 2 {
		t.Errorf("Expected 2 symbols in 15-minute group, got %d", len(grouped[15]))
	}

	if len(grouped[30]) != 1 {
		t.Errorf("Expected 1 symbol in 30-minute group, got %d", len(grouped[30]))
	}
}

func TestMinFunction(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{5, 10, 5},
		{10, 5, 5},
		{7, 7, 7},
		{0, 100, 0},
	}

	for _, tt := range tests {
		actual := min(tt.a, tt.b)
		if actual != tt.expected {
			t.Errorf("min(%d, %d) = %d, expected %d", tt.a, tt.b, actual, tt.expected)
		}
	}
}
