package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// shutdownWatchdogTimeout bounds the total time from a command's first
// shutdown signal to a forced process exit, if its own graceful shutdown
// hangs. `optix server` uses this: webui.Server.Start's HTTP drain is
// already bounded (5s), but the broker pool's Disconnect calls have no
// bound of their own in the worst case (see brokerPool.close in
// internal/webui/broker_pool.go) — ibapi's Disconnect can block forever
// against a wedged IB Gateway.
var shutdownWatchdogTimeout = 10 * time.Second

// armForceExitWatchdog restores two safety nets that `optix server` lost
// when SuppressSignalExit stopped the root package's global signal handler
// from racing the server's own signal.NotifyContext-driven shutdown (#196):
//
//  1. signal.NotifyContext's internal relay goroutine only forwards the
//     FIRST signal to ctx — it calls cancel() and exits right after, so a
//     second SIGINT/SIGTERM sent while shutdown is in progress was
//     previously delivered to nothing. This registers a fresh listener the
//     moment ctx is done, so a second signal forces an immediate exit.
//  2. The old root handler's 5s watchdog+os.Exit accidentally bounded total
//     shutdown time as a side effect of the race it lost. Now that the race
//     is gone, the graceful path itself has no timeout of its own past
//     webui.Server.Start's 5s HTTP drain — a hung broker Disconnect (or any
//     other future hang in the shutdown path) would block forever with no
//     second signal required to notice. The deadline arm covers that case
//     even if the operator never sends a second signal.
//
// Call this once, right after creating ctx via signal.NotifyContext — the
// returned stop func must be called once the command's own graceful
// shutdown has actually finished (typically via defer immediately after),
// so the watchdog does not fire after a shutdown that completed cleanly
// within the deadline.
func armForceExitWatchdog(ctx context.Context, deadline time.Duration) (stop func()) {
	stopCh := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-stopCh:
			return
		}
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigCh)
		watchShutdownDeadline(sigCh, deadline, stopCh)
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stopCh) }) }
}

// watchShutdownDeadline is the testable core of armForceExitWatchdog: given
// a signal channel, a deadline, and a stop channel, it calls osExit exactly
// once — with the signal's exit code if a signal arrives first, or the
// SIGTERM code if the deadline elapses first — unless stopCh is closed
// before either fires. Factored out of armForceExitWatchdog so tests can
// drive sigCh/stopCh directly instead of sending real OS signals into a
// shared test binary (which would also race any other signal.Notify
// listener already registered in that process, e.g. from other tests).
func watchShutdownDeadline(sigCh <-chan os.Signal, deadline time.Duration, stopCh <-chan struct{}) {
	select {
	case sig := <-sigCh:
		fmt.Fprintln(os.Stderr, "optix: second signal received during shutdown, forcing exit")
		osExit(signalExitCode(sig))
	case <-time.After(deadline):
		fmt.Fprintln(os.Stderr, "optix: graceful shutdown exceeded watchdog timeout, forcing exit")
		osExit(signalExitCode(syscall.SIGTERM))
	case <-stopCh:
	}
}
