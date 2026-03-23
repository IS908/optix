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
