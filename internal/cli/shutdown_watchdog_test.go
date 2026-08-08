package cli

import (
	"context"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

// withStubExit swaps osExit for a stub that records the exit code and
// signals a channel, restoring the original osExit on test cleanup. It does
// NOT touch signalExitSuppressed or cleanupItems — callers that also need
// those isolated should use withIsolatedSignalState from signal_test.go.
func withStubExit(t *testing.T) (calls chan int) {
	t.Helper()
	calls = make(chan int, 4)
	saved := osExit
	osExit = func(code int) { calls <- code }
	t.Cleanup(func() { osExit = saved })
	return calls
}

func awaitExit(t *testing.T, calls chan int, within time.Duration) int {
	t.Helper()
	select {
	case code := <-calls:
		return code
	case <-time.After(within):
		t.Fatalf("osExit was not called within %s", within)
		return -1
	}
}

func assertNoExit(t *testing.T, calls chan int, within time.Duration) {
	t.Helper()
	select {
	case code := <-calls:
		t.Fatalf("osExit(%d) was called, want no call", code)
	case <-time.After(within):
	}
}

// ─── watchShutdownDeadline (pure core, synthetic channels) ─────────────────

func TestWatchShutdownDeadlineForcesExitOnSecondSignal(t *testing.T) {
	calls := withStubExit(t)
	sigCh := make(chan os.Signal, 1)
	stopCh := make(chan struct{})

	done := make(chan struct{})
	go func() {
		watchShutdownDeadline(sigCh, time.Minute, stopCh)
		close(done)
	}()

	sigCh <- syscall.SIGTERM

	if code := awaitExit(t, calls, time.Second); code != 143 {
		t.Fatalf("osExit code = %d, want 143 (SIGTERM)", code)
	}
	<-done
}

func TestWatchShutdownDeadlineForcesExitOnSecondSignalSIGINT(t *testing.T) {
	calls := withStubExit(t)
	sigCh := make(chan os.Signal, 1)
	stopCh := make(chan struct{})

	go watchShutdownDeadline(sigCh, time.Minute, stopCh)
	sigCh <- syscall.SIGINT

	if code := awaitExit(t, calls, time.Second); code != 130 {
		t.Fatalf("osExit code = %d, want 130 (SIGINT)", code)
	}
}

func TestWatchShutdownDeadlineForcesExitOnTimeout(t *testing.T) {
	calls := withStubExit(t)
	sigCh := make(chan os.Signal, 1)
	stopCh := make(chan struct{})

	go watchShutdownDeadline(sigCh, 20*time.Millisecond, stopCh)

	if code := awaitExit(t, calls, time.Second); code != 143 {
		t.Fatalf("osExit code = %d, want 143 (deadline forces the SIGTERM code)", code)
	}
}

func TestWatchShutdownDeadlineStopPreventsExit(t *testing.T) {
	calls := withStubExit(t)
	sigCh := make(chan os.Signal, 1)
	stopCh := make(chan struct{})
	close(stopCh) // shutdown already finished before the watchdog ever races

	done := make(chan struct{})
	go func() {
		watchShutdownDeadline(sigCh, time.Minute, stopCh)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchShutdownDeadline did not return promptly on a pre-closed stopCh")
	}
	assertNoExit(t, calls, 50*time.Millisecond)
}

// ─── armForceExitWatchdog (ctx-gated wrapper, no real OS signals) ──────────

func TestArmForceExitWatchdogFiresOnDeadlineOnceCtxDone(t *testing.T) {
	calls := withStubExit(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // simulate the first signal already having fired

	stop := armForceExitWatchdog(ctx, 20*time.Millisecond)
	defer stop()

	if code := awaitExit(t, calls, time.Second); code != 143 {
		t.Fatalf("osExit code = %d, want 143", code)
	}
}

func TestArmForceExitWatchdogNeverFiresBeforeCtxDone(t *testing.T) {
	calls := withStubExit(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Short deadline, but ctx is never canceled — the deadline only starts
	// counting once ctx.Done() fires, so no exit should occur.
	stop := armForceExitWatchdog(ctx, 10*time.Millisecond)
	defer stop()

	assertNoExit(t, calls, 100*time.Millisecond)
}

// TestArmForceExitWatchdogStopSuppressesExit is the "graceful shutdown
// finished cleanly" case: ctx is done (first signal received) but the
// caller calls stop() (its own shutdown completed) before the deadline
// would otherwise fire — no exit should occur even though ctx.Done() had
// already unblocked the watchdog goroutine.
func TestArmForceExitWatchdogStopSuppressesExit(t *testing.T) {
	calls := withStubExit(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stop := armForceExitWatchdog(ctx, time.Second)
	stop()

	assertNoExit(t, calls, 200*time.Millisecond)
}

// TestArmForceExitWatchdogStopIsIdempotent guards against a panic (close of
// a closed channel) if stop is ever called more than once — server.go calls
// it via a single defer today, but the contract should hold regardless.
func TestArmForceExitWatchdogStopIsIdempotent(t *testing.T) {
	withStubExit(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := armForceExitWatchdog(ctx, time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stop()
		}()
	}
	wg.Wait()
}
