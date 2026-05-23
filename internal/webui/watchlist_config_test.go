package webui

import (
	"fmt"
	"testing"
)

// fakeWatchlistConfigUpdater lets us drive applyWatchlistConfig through known
// success/failure paths without touching the real SQLite store.
type fakeWatchlistConfigUpdater struct {
	failOn map[string]error // symbol → error to return (nil = success)
	calls  []string
}

func (f *fakeWatchlistConfigUpdater) UpdateWatchlistConfig(symbol string, _ bool, _ int) error {
	f.calls = append(f.calls, symbol)
	return f.failOn[symbol]
}

// TestApplyWatchlistConfig pins the contract for #37: per-symbol failures
// must be returned so the caller can log them, not silently swallowed by
// `_ = err` inside the batch loop. The pre-fix call site discarded these
// errors despite a "Log error but don't fail" comment that was never honored.
func TestApplyWatchlistConfig(t *testing.T) {
	t.Run("collects errors per symbol", func(t *testing.T) {
		fake := &fakeWatchlistConfigUpdater{
			failOn: map[string]error{
				"AAPL": fmt.Errorf("database is locked"),
				"MSFT": nil,
				"TSLA": fmt.Errorf("disk I/O error"),
			},
		}
		errs := applyWatchlistConfig(fake, []string{"AAPL", "MSFT", "TSLA"}, true, 15)
		if len(errs) != 2 {
			t.Fatalf("want 2 errors, got %d: %v", len(errs), errs)
		}
		if errs["AAPL"] == nil {
			t.Errorf("AAPL: expected error, got nil")
		}
		if errs["TSLA"] == nil {
			t.Errorf("TSLA: expected error, got nil")
		}
		if _, ok := errs["MSFT"]; ok {
			t.Errorf("MSFT should not appear in errs (no failure), got %v", errs["MSFT"])
		}
		if len(fake.calls) != 3 {
			t.Errorf("want 3 UpdateWatchlistConfig calls, got %d", len(fake.calls))
		}
	})

	t.Run("empty symbols → empty errs", func(t *testing.T) {
		fake := &fakeWatchlistConfigUpdater{}
		errs := applyWatchlistConfig(fake, nil, false, 0)
		if len(errs) != 0 {
			t.Errorf("want empty errs, got %v", errs)
		}
		if len(fake.calls) != 0 {
			t.Errorf("want 0 calls, got %d", len(fake.calls))
		}
	})

	t.Run("all symbols fail → all errors captured", func(t *testing.T) {
		want := fmt.Errorf("disk full")
		fake := &fakeWatchlistConfigUpdater{
			failOn: map[string]error{"NVDA": want, "AMD": want, "INTC": want},
		}
		errs := applyWatchlistConfig(fake, []string{"NVDA", "AMD", "INTC"}, true, 30)
		if len(errs) != 3 {
			t.Fatalf("want 3 errors, got %d", len(errs))
		}
		for _, s := range []string{"NVDA", "AMD", "INTC"} {
			if errs[s] == nil {
				t.Errorf("%s: expected error, got nil", s)
			}
		}
	})

	t.Run("does not stop on first error", func(t *testing.T) {
		// Critical invariant: if symbol[0] errors, symbols[1] and [2] must
		// still be attempted. Pre-fix the loop continued (correct); locking
		// it in a test prevents a refactor from accidentally introducing
		// an early return.
		fake := &fakeWatchlistConfigUpdater{
			failOn: map[string]error{"AAPL": fmt.Errorf("locked")},
		}
		_ = applyWatchlistConfig(fake, []string{"AAPL", "MSFT", "TSLA"}, true, 15)
		if len(fake.calls) != 3 {
			t.Errorf("want all 3 symbols attempted despite first failure, got %d calls: %v",
				len(fake.calls), fake.calls)
		}
	})
}
