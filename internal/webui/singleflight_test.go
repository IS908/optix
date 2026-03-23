package webui

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSingleflightAnalyzeDeduplication verifies that concurrent calls to
// sfGroup.Do("analyze:<symbol>") deduplicate correctly: the inner function
// runs exactly once regardless of how many callers arrive simultaneously.
//
// This mirrors fetchLiveAnalysis, which uses s.sfGroup.Do("analyze:"+symbol, ...).
func TestSingleflightAnalyzeDeduplication(t *testing.T) {
	s := &Server{} // zero-value sfGroup is valid

	const goroutines = 10
	var callCount int64

	// barrier ensures all goroutines hit sfGroup.Do at the same instant.
	var barrier sync.WaitGroup
	barrier.Add(goroutines)

	slowFn := func() (any, error) {
		atomic.AddInt64(&callCount, 1)
		time.Sleep(40 * time.Millisecond) // hold long enough for all goroutines to pile up
		return "result", nil
	}

	var wg sync.WaitGroup
	results := make([]any, goroutines)
	errors := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			barrier.Done()
			barrier.Wait() // all goroutines release together
			results[idx], errors[idx], _ = s.sfGroup.Do("analyze:AAPL", slowFn)
		}(i)
	}
	wg.Wait()

	for i, err := range errors {
		if err != nil {
			t.Errorf("goroutine %d got unexpected error: %v", i, err)
		}
	}
	if callCount != 1 {
		t.Errorf("inner function ran %d times, want exactly 1 (singleflight not deduplicating)", callCount)
	}
	for i, v := range results {
		if v != "result" {
			t.Errorf("goroutine %d got result %v, want \"result\"", i, v)
		}
	}
}

// TestSingleflightDifferentSymbolsIndependent verifies that different symbols
// are NOT deduplicated — each symbol must get its own independent fetch.
func TestSingleflightDifferentSymbolsIndependent(t *testing.T) {
	s := &Server{}

	symbols := []string{"AAPL", "TSLA", "NVDA"}
	var callCount int64
	var wg sync.WaitGroup

	for _, sym := range symbols {
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			s.sfGroup.Do("analyze:"+symbol, func() (any, error) {
				atomic.AddInt64(&callCount, 1)
				time.Sleep(20 * time.Millisecond)
				return nil, nil
			})
		}(sym)
	}
	wg.Wait()

	if callCount != int64(len(symbols)) {
		t.Errorf("inner function ran %d times, want %d (one per symbol)", callCount, len(symbols))
	}
}

// TestSingleflightDashboardDeduplication verifies that concurrent dashboard
// refreshes are deduplicated under the shared "dashboard" key.
//
// This mirrors fetchLiveDashboard, which uses s.sfGroup.Do("dashboard", ...).
func TestSingleflightDashboardDeduplication(t *testing.T) {
	s := &Server{}

	const goroutines = 8
	var callCount int64
	var barrier sync.WaitGroup
	barrier.Add(goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			barrier.Done()
			barrier.Wait()
			s.sfGroup.Do("dashboard", func() (any, error) {
				atomic.AddInt64(&callCount, 1)
				time.Sleep(40 * time.Millisecond)
				return nil, nil
			})
		}()
	}
	wg.Wait()

	if callCount != 1 {
		t.Errorf("dashboard fetch ran %d times, want exactly 1 (singleflight not deduplicating)", callCount)
	}
}

// TestSingleflightAnalyzeAndDashboardIndependent verifies that the "analyze:<sym>"
// and "dashboard" keys are independent — both can run concurrently.
func TestSingleflightAnalyzeAndDashboardIndependent(t *testing.T) {
	s := &Server{}

	var analyzeCalls, dashCalls int64
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		s.sfGroup.Do("analyze:AAPL", func() (any, error) {
			atomic.AddInt64(&analyzeCalls, 1)
			time.Sleep(30 * time.Millisecond)
			return nil, nil
		})
	}()
	go func() {
		defer wg.Done()
		s.sfGroup.Do("dashboard", func() (any, error) {
			atomic.AddInt64(&dashCalls, 1)
			time.Sleep(30 * time.Millisecond)
			return nil, nil
		})
	}()
	wg.Wait()

	if analyzeCalls != 1 {
		t.Errorf("analyze inner fn ran %d times, want 1", analyzeCalls)
	}
	if dashCalls != 1 {
		t.Errorf("dashboard inner fn ran %d times, want 1", dashCalls)
	}
}
