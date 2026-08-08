package cli

import (
	"syscall"
	"testing"
)

// withIsolatedSignalState resets the package-level cleanup registry and
// signalExitSuppressed flag for the duration of a test, restoring the prior
// values afterward. osExit is swapped for a caller-supplied stub and also
// restored. This lets tests drive handleSignal directly without touching
// real OS signals or actually exiting the test binary.
func withIsolatedSignalState(t *testing.T, stub func(code int)) {
	t.Helper()
	cleanupMu.Lock()
	savedItems := cleanupItems
	cleanupItems = nil
	cleanupMu.Unlock()

	savedSuppressed := signalExitSuppressed.Load()
	savedExit := osExit
	osExit = stub

	t.Cleanup(func() {
		cleanupMu.Lock()
		cleanupItems = savedItems
		cleanupMu.Unlock()
		signalExitSuppressed.Store(savedSuppressed)
		osExit = savedExit
	})
}

// TestHandleSignalExitsAndCleansUpByDefault covers the pre-#196 behavior:
// with no command suppressing it, a SIGTERM drains the cleanup registry and
// exits with the signal-appropriate code.
func TestHandleSignalExitsAndCleansUpByDefault(t *testing.T) {
	var gotCode int
	exited := false
	withIsolatedSignalState(t, func(code int) {
		gotCode = code
		exited = true
	})
	signalExitSuppressed.Store(false)

	n := 0
	RegisterCleanup(fakeCloser{closed: &n})

	handleSignal(syscall.SIGTERM)

	if !exited {
		t.Fatal("osExit was not called")
	}
	if gotCode != 143 {
		t.Fatalf("osExit code = %d, want 143 (SIGTERM)", gotCode)
	}
	if n != 1 {
		t.Fatalf("cleanup ran %d times, want 1", n)
	}
}

// TestHandleSignalNoOpWhenSuppressed is the #196 regression test: once
// SuppressSignalExit has been called (as `optix server` now does before
// installing its own signal.NotifyContext-driven shutdown), the root
// handler must neither drain the cleanup registry nor call os.Exit —
// otherwise it can race and truncate the server's own graceful shutdown.
func TestHandleSignalNoOpWhenSuppressed(t *testing.T) {
	exited := false
	withIsolatedSignalState(t, func(code int) { exited = true })

	SuppressSignalExit()
	if !signalExitSuppressed.Load() {
		t.Fatal("SuppressSignalExit did not set the flag")
	}

	n := 0
	RegisterCleanup(fakeCloser{closed: &n})

	handleSignal(syscall.SIGTERM)

	if exited {
		t.Fatal("osExit was called despite SuppressSignalExit")
	}
	if n != 0 {
		t.Fatalf("cleanup ran %d times, want 0 — suppressed handler must not touch the registry", n)
	}

	cleanupMu.Lock()
	remaining := len(cleanupItems)
	cleanupMu.Unlock()
	if remaining != 1 {
		t.Fatalf("cleanupItems len = %d, want 1 (left untouched for the owning command's own shutdown flow)", remaining)
	}
}

// TestSuppressSignalExitSetsFlag is a narrow unit check on the flag itself,
// independent of handleSignal's behavior.
func TestSuppressSignalExitSetsFlag(t *testing.T) {
	saved := signalExitSuppressed.Load()
	t.Cleanup(func() { signalExitSuppressed.Store(saved) })

	signalExitSuppressed.Store(false)
	SuppressSignalExit()
	if !signalExitSuppressed.Load() {
		t.Fatal("SuppressSignalExit did not set signalExitSuppressed")
	}
}
