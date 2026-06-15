package webui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/pkg/model"
)

// ─── mock broker ──────────────────────────────────────────────────────────────

// mockBroker implements broker.Broker for testing without touching the network.
type mockBroker struct {
	mu           sync.Mutex
	connected    bool
	connectErr   error
	connectCalls int32
	disconnCalls int32
}

func (m *mockBroker) Connect(_ context.Context) error {
	atomic.AddInt32(&m.connectCalls, 1)
	if m.connectErr != nil {
		return m.connectErr
	}
	m.mu.Lock()
	m.connected = true
	m.mu.Unlock()
	return nil
}

func (m *mockBroker) Disconnect() error {
	atomic.AddInt32(&m.disconnCalls, 1)
	m.mu.Lock()
	m.connected = false
	m.mu.Unlock()
	return nil
}

func (m *mockBroker) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

// drop simulates TWS silently dropping the TCP connection.
func (m *mockBroker) drop() {
	m.mu.Lock()
	m.connected = false
	m.mu.Unlock()
}

func (*mockBroker) GetQuote(_ context.Context, _ string) (*model.StockQuote, error) {
	return nil, nil
}
func (*mockBroker) GetHistoricalBars(_ context.Context, _, _, _, _ string) ([]model.OHLCV, error) {
	return nil, nil
}
func (*mockBroker) GetOptionChain(_ context.Context, _ string, _ string) (*model.OptionChain, error) {
	return nil, nil
}

// Ensure compile-time satisfaction of broker.Broker.
var _ broker.Broker = (*mockBroker)(nil)

// fallbackMock is a connected broker that reports it is running on the degraded
// (yfinance) fallback source, so it exercises the pool's IBKR switchback path
// without needing a real *broker.FallbackBroker.
type fallbackMock struct{ mockBroker }

func (*fallbackMock) UsingFallback() bool { return true }

var _ fallbackReporter = (*fallbackMock)(nil)

// liveMock is a connected broker that reports it is NOT on the fallback (i.e.
// live on IBKR), used to exercise the backoff-clearing branch.
type liveMock struct{ mockBroker }

func (*liveMock) UsingFallback() bool { return false }

// testClock is a race-free injectable clock. The pool's health-checker goroutine
// may read it concurrently with a test advancing it, so the time is held in an
// atomic rather than a plain field.
type testClock struct{ nanos atomic.Int64 }

func newTestClock(t time.Time) *testClock {
	c := &testClock{}
	c.nanos.Store(t.UnixNano())
	return c
}

func (c *testClock) now() time.Time          { return time.Unix(0, c.nanos.Load()) }
func (c *testClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }

// ─── factory helpers ──────────────────────────────────────────────────────────

// alwaysConnectedFactory returns a brokerFactory that creates a new already-
// connected mockBroker on every call and increments *created.
func alwaysConnectedFactory(created *int32) brokerFactory {
	return func(_ context.Context, _ int64) (broker.Broker, error) {
		atomic.AddInt32(created, 1)
		return &mockBroker{connected: true}, nil
	}
}

// sequentialFactory returns a brokerFactory that hands out the provided mocks
// in order and calls Connect() on each one (mirroring the real factory).
func sequentialFactory(mocks []*mockBroker) brokerFactory {
	var idx int32
	return func(ctx context.Context, _ int64) (broker.Broker, error) {
		i := int(atomic.AddInt32(&idx, 1)) - 1
		if i >= len(mocks) {
			return nil, fmt.Errorf("sequentialFactory: exhausted (index %d)", i)
		}
		m := mocks[i]
		if err := m.Connect(ctx); err != nil {
			return nil, err
		}
		return m, nil
	}
}

// errorFactory returns a brokerFactory that always fails to connect.
func errorFactory() brokerFactory {
	return func(_ context.Context, _ int64) (broker.Broker, error) {
		return nil, fmt.Errorf("intentional connect failure")
	}
}

// ─── tests ────────────────────────────────────────────────────────────────────

// TestPoolConcurrentLimit verifies the semaphore: at most cap() goroutines can
// hold a slot simultaneously no matter how many callers compete.
func TestPoolConcurrentLimit(t *testing.T) {
	const poolSize = 3
	const goroutines = 12

	var created int32
	pool := newBrokerPool(poolSize, alwaysConnectedFactory(&created))
	defer pool.close()

	var (
		activeNow  int64
		peakActive int64
		peakMu     sync.Mutex
		wg         sync.WaitGroup
		barrier    sync.WaitGroup
	)
	barrier.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			barrier.Done()
			barrier.Wait() // all goroutines release at the same instant

			conn, err := pool.acquire(context.Background())
			if err != nil {
				t.Errorf("acquire error: %v", err)
				return
			}
			cur := atomic.AddInt64(&activeNow, 1)
			peakMu.Lock()
			if cur > peakActive {
				peakActive = cur
			}
			peakMu.Unlock()

			time.Sleep(20 * time.Millisecond) // hold the slot briefly

			atomic.AddInt64(&activeNow, -1)
			pool.release(conn, true)
		}()
	}
	wg.Wait()

	if peakActive > int64(poolSize) {
		t.Errorf("peak concurrent holders = %d, exceeded pool size %d", peakActive, poolSize)
	}
}

// TestPoolClientIDsUnique verifies that all slots carry distinct ClientIDs in
// the range [poolClientIDBase, poolClientIDBase+size).
func TestPoolClientIDsUnique(t *testing.T) {
	const poolSize = 5
	var created int32
	pool := newBrokerPool(poolSize, alwaysConnectedFactory(&created))
	defer pool.close()

	ctx := context.Background()
	conns := make([]*pooledConn, poolSize)
	for i := range conns {
		c, err := pool.acquire(ctx)
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
		conns[i] = c
	}

	seen := make(map[int64]bool, poolSize)
	for i, c := range conns {
		if seen[c.id] {
			t.Errorf("slot %d: duplicate ClientID %d", i, c.id)
		}
		seen[c.id] = true
		if c.id < poolClientIDBase || c.id >= int64(poolClientIDBase+poolSize) {
			t.Errorf("slot %d: ClientID %d out of range [%d, %d)",
				i, c.id, poolClientIDBase, poolClientIDBase+poolSize)
		}
		pool.release(c, true)
	}
}

// TestPoolContextCancelOnExhaustion verifies that acquire returns an error when
// the pool is full and the caller's context is cancelled.
func TestPoolContextCancelOnExhaustion(t *testing.T) {
	var created int32
	pool := newBrokerPool(1, alwaysConnectedFactory(&created))
	defer pool.close()

	// Exhaust the single slot.
	held, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer pool.release(held, true)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = pool.acquire(ctx)
	if err == nil {
		t.Fatal("expected error when pool exhausted and context timed out, got nil")
	}
}

// TestPoolReleaseUnhealthyTriggersReconnect verifies that releasing a slot with
// healthy=false causes an async reconnect (factory called again) before the
// slot re-enters the pool.
func TestPoolReleaseUnhealthyTriggersReconnect(t *testing.T) {
	var created int32
	pool := newBrokerPool(1, alwaysConnectedFactory(&created))
	defer pool.close()

	ctx := context.Background()

	// Acquire — factory creates broker #1.
	conn, err := pool.acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	firstID := conn.id

	// Release as unhealthy → async reconnect.
	pool.release(conn, false)

	// Wait for the slot to return to the pool.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pool.available() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pool.available() != 1 {
		t.Fatal("slot not returned to pool after async reconnect timeout")
	}

	// Re-acquire and confirm it's healthy with the same ClientID.
	conn2, err := pool.acquire(ctx)
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	if !conn2.isConnected() {
		t.Error("slot not healthy after async reconnect")
	}
	if conn2.id != firstID {
		t.Errorf("ClientID changed after reconnect: was %d, now %d", firstID, conn2.id)
	}
	pool.release(conn2, true)

	// Factory must have been invoked at least twice.
	if atomic.LoadInt32(&created) < 2 {
		t.Errorf("factory called %d times, want ≥ 2 (lazy init + reconnect)", created)
	}
}

// TestPoolHealthCheckReconnectsDropped verifies that checkIdleConnections
// reconnects a slot whose broker has silently disconnected (TCP zombie / TWS restart).
func TestPoolHealthCheckReconnectsDropped(t *testing.T) {
	mock0 := &mockBroker{}
	mock1 := &mockBroker{}
	pool := newBrokerPool(1, sequentialFactory([]*mockBroker{mock0, mock1}))
	defer pool.close()

	ctx := context.Background()

	// Warm up: acquire so mock0 is created, then release back to the pool.
	conn, err := pool.acquire(ctx)
	if err != nil {
		t.Fatalf("initial acquire: %v", err)
	}
	pool.release(conn, true)

	// Simulate TWS dropping the connection silently (no callback to app layer).
	mock0.drop()
	if mock0.IsConnected() {
		t.Fatal("mock0 should be disconnected after drop()")
	}

	// Manually invoke the health checker (avoids waiting 30 s for the ticker).
	pool.checkIdleConnections()

	// checkIdleConnections calls reconnect synchronously in the test goroutine.
	// Give it a tiny moment for the goroutine-free path to complete.
	time.Sleep(20 * time.Millisecond)

	// The slot should now be healthy.
	conn2, err := pool.acquire(ctx)
	if err != nil {
		t.Fatalf("acquire after health check: %v", err)
	}
	if !conn2.isConnected() {
		t.Error("slot not healthy after checkIdleConnections")
	}
	pool.release(conn2, true)

	// mock1 was the replacement broker — it must have been connected.
	if atomic.LoadInt32(&mock1.connectCalls) == 0 {
		t.Error("mock1.connectCalls == 0: health check did not trigger reconnect via factory")
	}
}

// TestPoolSwitchbackBackoff verifies that when a slot is stuck on the yfinance
// fallback (IBKR unreachable), the health checker does NOT re-attempt the IBKR
// switchback on every cycle — it backs off exponentially. This prevents the
// reconnect storm that hammers TWS's 32-connection limit and repeatedly drives
// the racy ibapi connect/teardown path (see #171).
func TestPoolSwitchbackBackoff(t *testing.T) {
	var created int32
	// Factory always returns a broker that reports it's on the degraded fallback,
	// so every switchback attempt "fails" to reach IBKR and stays on fallback.
	factory := func(_ context.Context, _ int64) (broker.Broker, error) {
		atomic.AddInt32(&created, 1)
		return &fallbackMock{mockBroker{connected: true}}, nil
	}

	clock := newTestClock(time.Unix(1_700_000_000, 0))
	pool := newBrokerPoolWithClock(1, factory, clock.now)
	defer pool.close()

	// Warm up: acquire creates broker #1 (on fallback), release back to pool.
	conn, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	pool.release(conn, true)
	base := atomic.LoadInt32(&created) // factory calls so far (lazy init)

	// First health cycle: nextRetry is unset → switchback attempt fires once,
	// then sets a backoff window of backoffFor(1) from now.
	pool.checkIdleConnections()
	if got := atomic.LoadInt32(&created) - base; got != 1 {
		t.Fatalf("first health cycle: switchback attempts = %d, want 1", got)
	}

	// Within the backoff window: no further attempts, no matter how many health
	// cycles run (this is the storm that #171 must prevent).
	clock.advance(backoffFor(1) - time.Second)
	for i := 0; i < 3; i++ {
		pool.checkIdleConnections()
	}
	if got := atomic.LoadInt32(&created) - base; got != 1 {
		t.Fatalf("during backoff window: switchback attempts = %d, want 1 (no storm)", got)
	}

	// Once the backoff window elapses, exactly one more attempt is allowed.
	clock.advance(2 * time.Second) // now past nextRetry
	pool.checkIdleConnections()
	if got := atomic.LoadInt32(&created) - base; got != 2 {
		t.Fatalf("after backoff elapsed: switchback attempts = %d, want 2", got)
	}
}

// TestBackoffFor verifies the exponential schedule and that it saturates at the
// cap rather than overflowing.
func TestBackoffFor(t *testing.T) {
	cases := []struct {
		failCount int
		want      time.Duration
	}{
		{-1, 0},
		{0, 0},
		{1, 2 * healthCheckInterval},  // 60s
		{2, 4 * healthCheckInterval},  // 120s
		{3, 8 * healthCheckInterval},  // 240s
		{4, 16 * healthCheckInterval}, // 480s
		{5, maxReconnectBackoff},      // 960s ≥ 900s cap → capped
		{6, maxReconnectBackoff},
		{100, maxReconnectBackoff}, // no overflow, stays capped
	}
	for _, c := range cases {
		if got := backoffFor(c.failCount); got != c.want {
			t.Errorf("backoffFor(%d) = %v, want %v", c.failCount, got, c.want)
		}
	}
}

// TestPoolSwitchbackRecoveryStampsConnectedSince verifies the recovery half of
// trackReconnectOutcome: once a slot reconnects live on IBKR (not on the
// fallback), its dwell timer starts so a subsequent stable health cycle can
// clear the backoff (#176). Note that backoff is NOT cleared on the immediate
// success; it requires dwellWindow of stable connection — see
// TestPoolBackoffClearsAfterDwell.
func TestPoolSwitchbackRecoveryStampsConnectedSince(t *testing.T) {
	// First reconnect lands on fallback; the second comes back live on IBKR.
	var calls int32
	factory := func(_ context.Context, _ int64) (broker.Broker, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return &fallbackMock{mockBroker{connected: true}}, nil
		}
		return &liveMock{mockBroker{connected: true}}, nil
	}

	clock := newTestClock(time.Unix(1_700_000_000, 0))
	pool := newBrokerPoolWithClock(1, factory, clock.now)
	defer pool.close()

	conn, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	pool.release(conn, true)

	pool.checkIdleConnections()
	// Switchback fired and produced a live mock. failCount was never grown (no
	// failure occurred), so it stays 0. connectedSince must now be stamped so
	// the dwell timer can run on subsequent cycles.
	if conn.connectedSince.IsZero() {
		t.Errorf("connectedSince not stamped after live-IBKR reconnect")
	}
}

// TestPoolBackoffClearsAfterDwell verifies that a slot's backoff state clears
// only after it has held a live IBKR connection for ≥ dwellWindow. A single
// brief success must NOT clear the backoff — that was the flapping-IBKR bug
// the dwell-time fix addresses (#176).
func TestPoolBackoffClearsAfterDwell(t *testing.T) {
	clock := newTestClock(time.Unix(1_700_000_000, 0))
	// Factory always returns a live IBKR mock; the test seeds backoff state
	// directly to skip the failure setup and focus on the dwell semantics.
	pool := newBrokerPoolWithClock(1, func(_ context.Context, _ int64) (broker.Broker, error) {
		return &liveMock{mockBroker{connected: true}}, nil
	}, clock.now)
	defer pool.close()

	// Warm up: acquire creates the broker.
	conn, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	pool.release(conn, true)
	// Seed elevated backoff state — as if a prior failure storm had grown it.
	conn.failCount = 3
	conn.nextRetry = clock.now().Add(time.Minute)
	conn.connectedSince = time.Time{} // not yet observed live

	// First healthy cycle: stamps connectedSince. Backoff NOT yet cleared.
	pool.checkIdleConnections()
	if conn.connectedSince.IsZero() {
		t.Fatalf("first healthy cycle: connectedSince not stamped")
	}
	if conn.failCount != 3 || conn.nextRetry.IsZero() {
		t.Errorf("first cycle (no dwell yet): failCount=%d nextRetry=%v, want 3/non-zero",
			conn.failCount, conn.nextRetry)
	}

	// Advance by less than dwellWindow → no clear yet.
	clock.advance(dwellWindow - time.Second)
	pool.checkIdleConnections()
	if conn.failCount != 3 {
		t.Errorf("within dwell window: failCount=%d, want 3 (not cleared yet)", conn.failCount)
	}

	// Advance past dwellWindow → backoff clears.
	clock.advance(2 * time.Second)
	pool.checkIdleConnections()
	if conn.failCount != 0 || !conn.nextRetry.IsZero() {
		t.Errorf("after dwell elapsed: failCount=%d nextRetry=%v, want 0/zero",
			conn.failCount, conn.nextRetry)
	}
}

// TestPoolFlappingDoesNotClearBackoff verifies the load-bearing claim of #176:
// a flapping IBKR (handshake-then-drop) cannot use a single brief success to
// reset the backoff growth. The slot must stay stable for ≥ dwellWindow before
// the backoff relaxes.
func TestPoolFlappingDoesNotClearBackoff(t *testing.T) {
	clock := newTestClock(time.Unix(1_700_000_000, 0))
	pool := newBrokerPoolWithClock(1, func(_ context.Context, _ int64) (broker.Broker, error) {
		return &liveMock{mockBroker{connected: true}}, nil
	}, clock.now)
	defer pool.close()

	conn, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	pool.release(conn, true)
	// Seed elevated backoff state.
	conn.failCount = 3
	conn.nextRetry = clock.now().Add(time.Minute)

	// Healthy cycle: stamps connectedSince (no clear yet — dwell not elapsed).
	pool.checkIdleConnections()

	// Simulate flap — the connection drops before dwell elapses. We mimic this
	// by zeroing connectedSince (what trackReconnectOutcome does after a failed
	// reconnect) without advancing the clock past dwellWindow.
	conn.connectedSince = time.Time{}

	// Subsequent healthy cycle (still within what would have been the dwell):
	// connectedSince is re-stamped fresh — the dwell timer restarts. Backoff
	// must NOT have cleared.
	clock.advance(dwellWindow / 2)
	pool.checkIdleConnections()
	if conn.failCount != 3 {
		t.Errorf("after flap reset: failCount=%d, want 3 (not cleared by brief success)", conn.failCount)
	}
}

// TestPoolUnhealthyReconnectGatedByBackoff verifies the second half of #176:
// the genuine-unhealthy reconnect path now respects the backoff schedule, so a
// repeatedly-failing ping doesn't drive a reconnect every 30s cycle.
func TestPoolUnhealthyReconnectGatedByBackoff(t *testing.T) {
	var created int32
	clock := newTestClock(time.Unix(1_700_000_000, 0))
	pool := newBrokerPoolWithClock(1, func(_ context.Context, _ int64) (broker.Broker, error) {
		atomic.AddInt32(&created, 1)
		// Return a disconnected mock so the slot stays unhealthy.
		return &mockBroker{connected: false}, nil
	}, clock.now)
	defer pool.close()

	conn, err := pool.acquire(context.Background())
	// acquire-time reconnect returns the disconnected mock; acquire's own
	// reconnect path is ungated and constructs broker #1 here.
	if err == nil {
		pool.release(conn, true)
	}
	base := atomic.LoadInt32(&created)

	// Force the slot into a backoff window: failCount=2 → nextRetry well in the future.
	if conn != nil {
		conn.failCount = 2
		conn.nextRetry = clock.now().Add(time.Hour)
	}

	// Run several health-checker cycles within the backoff window.
	for i := 0; i < 5; i++ {
		pool.checkIdleConnections()
		clock.advance(healthCheckInterval)
	}
	if got := atomic.LoadInt32(&created) - base; got != 0 {
		t.Errorf("within backoff window: %d unhealthy reconnects, want 0 (gate must block them)", got)
	}
}

// TestPoolHealthyIBKRNeverSwitchesBack verifies that a slot already live on IBKR
// (not on the fallback) is never reconnected by the health checker, no matter
// how many cycles run — the switchback short-circuits on UsingFallback()==false.
func TestPoolHealthyIBKRNeverSwitchesBack(t *testing.T) {
	var created int32
	factory := func(_ context.Context, _ int64) (broker.Broker, error) {
		atomic.AddInt32(&created, 1)
		return &liveMock{mockBroker{connected: true}}, nil
	}

	clock := newTestClock(time.Unix(1_700_000_000, 0))
	pool := newBrokerPoolWithClock(1, factory, clock.now)
	defer pool.close()

	conn, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	pool.release(conn, true)
	base := atomic.LoadInt32(&created)

	for i := 0; i < 5; i++ {
		clock.advance(healthCheckInterval)
		pool.checkIdleConnections()
	}
	if got := atomic.LoadInt32(&created) - base; got != 0 {
		t.Errorf("healthy IBKR slot: reconnect attempts = %d, want 0", got)
	}
}

// TestPoolLazyInit verifies that connections are created lazily on first acquire
// (not at pool construction time).
func TestPoolLazyInit(t *testing.T) {
	var created int32
	pool := newBrokerPool(4, alwaysConnectedFactory(&created))
	defer pool.close()

	if n := atomic.LoadInt32(&created); n != 0 {
		t.Fatalf("expected 0 factory calls at construction, got %d", n)
	}

	conn, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if n := atomic.LoadInt32(&created); n != 1 {
		t.Errorf("expected 1 factory call after first acquire, got %d", n)
	}
	if !conn.isConnected() {
		t.Error("acquired slot not connected")
	}
	pool.release(conn, true)
}

// TestPoolConnectFailurePreservesCapacity verifies that when acquire fails because
// the factory errors, the slot is returned to the pool (capacity is preserved).
func TestPoolConnectFailurePreservesCapacity(t *testing.T) {
	pool := newBrokerPool(1, errorFactory())
	defer pool.close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := pool.acquire(ctx)
	if err == nil {
		t.Fatal("expected error from errorFactory, got nil")
	}

	if pool.available() != 1 {
		t.Errorf("pool.available() = %d after failed acquire, want 1", pool.available())
	}
}

// TestPoolCapDefaults verifies cap() and that size ≤ 0 falls back to defaultPoolSize.
func TestPoolCapDefaults(t *testing.T) {
	var created int32
	f := alwaysConnectedFactory(&created)

	pDefault := newBrokerPool(0, f)
	defer pDefault.close()
	if pDefault.cap() != defaultPoolSize {
		t.Errorf("size=0: cap=%d, want %d (defaultPoolSize)", pDefault.cap(), defaultPoolSize)
	}

	p3 := newBrokerPool(3, f)
	defer p3.close()
	if p3.cap() != 3 {
		t.Errorf("size=3: cap=%d, want 3", p3.cap())
	}
}
