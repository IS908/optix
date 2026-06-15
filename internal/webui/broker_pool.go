package webui

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
)

const (
	// poolClientIDBase is the first ClientID reserved for the web UI broker pool.
	// CLI uses 1–6; scheduler workers use 10–14 (10 + worker.id).
	// Starting at 30 leaves ample headroom for future expansion.
	poolClientIDBase = 30

	// defaultPoolSize is the default number of concurrent broker connections.
	// Each slot holds one IBKR ClientID; TWS allows up to 32 total connections.
	defaultPoolSize = 8

	// reconnectTimeout caps the time spent reconnecting a single slot.
	reconnectTimeout = 15 * time.Second

	// healthCheckInterval is how often the background goroutine probes idle slots.
	healthCheckInterval = 30 * time.Second

	// maxReconnectBackoff caps the exponential backoff applied to a slot that
	// repeatedly fails to reach IBKR (and lands on the yfinance fallback). It
	// bounds how rarely the switchback probe runs while IBKR stays unreachable.
	maxReconnectBackoff = 15 * time.Minute

	// dwellWindow is how long a slot must hold a live IBKR connection before the
	// background health checker treats the recovery as stable and clears the
	// backoff. A flapping gateway (handshake then drop) thus cannot reset the
	// backoff via a momentary success — the slot has to stay alive for ≥dwellWindow
	// across health cycles before failCount/nextRetry clear. #176
	dwellWindow = 2 * healthCheckInterval
)

// fallbackReporter is implemented by brokers that can report whether they are
// currently serving the degraded (yfinance) fallback source. *broker.FallbackBroker
// satisfies it; the pool uses it to decide when to attempt an IBKR switchback.
type fallbackReporter interface {
	UsingFallback() bool
}

// brokerFactory creates an already-connected broker for the given clientID.
// The production implementation dials IBKR (with yfinance fallback); tests
// inject a mock that avoids real network calls.
type brokerFactory func(ctx context.Context, clientID int64) (broker.Broker, error)

// pooledConn is a single managed connection slot inside brokerPool.
//
// failCount/nextRetry/connectedSince implement per-slot reconnect backoff. They
// are only mutated while the slot is exclusively owned (drained out of the
// avail channel), so they need no extra locking — same ownership discipline as
// lastUsed.
type pooledConn struct {
	id       int64      // IBKR ClientID (fixed for the lifetime of the slot)
	mu       sync.Mutex // guards b; only held during reconnect
	b        broker.Broker
	lastUsed time.Time

	failCount int       // consecutive IBKR-reconnect failures (slot stuck on fallback)
	nextRetry time.Time // earliest time the health checker may re-probe IBKR
	// connectedSince stamps when the current live-IBKR connection was first
	// observed by the health checker (or established by reconnect). The dwell
	// timer; see clearBackoffIfStable. Zero when the slot is on fallback,
	// disconnected, or hasn't been observed live by the background checker. #176
	connectedSince time.Time
}

// sourceName returns a display-friendly data-source label.
// It type-asserts to *broker.FallbackBroker to surface IBKR vs yfinance.
func (c *pooledConn) sourceName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if fb, ok := c.b.(*broker.FallbackBroker); ok {
		return fb.SourceName()
	}
	return "IBKR"
}

// isConnected reports whether the slot's broker is alive (lock-free read during
// sole ownership between acquire and release).
func (c *pooledConn) isConnected() bool {
	return c.b != nil && c.b.IsConnected()
}

// brokerPool manages a bounded set of persistent IBKR broker connections for
// the web UI. It serves two purposes:
//
//  1. Semaphore — at most cap() concurrent TWS connections are open at once,
//     avoiding TWS's 32-connection limit.
//
//  2. ClientID uniqueness — each slot has its own ClientID (30, 31, …), so
//     concurrent requests for different symbols never collide.
//
// Connections are created lazily (on first acquire) and kept alive across
// requests. If a connection drops (TWS restart, TCP zombie), release() triggers
// an async reconnect; the background health-checker catches any that are missed.
type brokerPool struct {
	factory brokerFactory
	conns   []*pooledConn    // fixed slice; index i → ClientID poolClientIDBase+i
	avail   chan *pooledConn // buffered channel acting as both semaphore and queue
	ctx     context.Context
	cancel  context.CancelFunc
	now     func() time.Time // injectable clock (tests); defaults to time.Now
}

// newBrokerPool initialises a pool of size slots.
// size ≤ 0 falls back to defaultPoolSize.
// The pool's background health-checker runs until close() is called.
func newBrokerPool(size int, factory brokerFactory) *brokerPool {
	return newBrokerPoolWithClock(size, factory, time.Now)
}

// newBrokerPoolWithClock is newBrokerPool with an injectable clock. The clock is
// set before the health-checker goroutine starts (so p.now is written exactly
// once, never concurrently with a read). Tests use this to drive backoff timing
// deterministically.
func newBrokerPoolWithClock(size int, factory brokerFactory, now func() time.Time) *brokerPool {
	if size <= 0 {
		size = defaultPoolSize
	}
	conns := make([]*pooledConn, size)
	avail := make(chan *pooledConn, size)
	for i := 0; i < size; i++ {
		c := &pooledConn{id: int64(poolClientIDBase + i)}
		conns[i] = c
		avail <- c // all slots start as available (not yet connected)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &brokerPool{
		factory: factory,
		conns:   conns,
		avail:   avail,
		ctx:     ctx,
		cancel:  cancel,
		now:     now,
	}
	go p.healthChecker()
	return p
}

// backoffFor returns the reconnect backoff for a slot with failCount consecutive
// IBKR-reconnect failures: 2×healthCheckInterval, doubling each further failure,
// capped at maxReconnectBackoff. The first failure already throttles past the
// normal 30s health cycle (so a stuck slot skips cycles rather than retrying
// every one). failCount ≤ 0 yields 0 (no backoff).
func backoffFor(failCount int) time.Duration {
	if failCount <= 0 {
		return 0
	}
	d := healthCheckInterval
	for i := 0; i < failCount; i++ {
		d *= 2
		if d >= maxReconnectBackoff {
			return maxReconnectBackoff
		}
	}
	return d
}

// trackReconnectOutcome updates a slot's backoff state after a (re)connect
// attempt. A slot that came back live on IBKR stamps connectedSince to start
// the dwell timer — backoff is NOT cleared yet; clearBackoffIfStable will, on a
// subsequent health cycle, once the slot has held the connection for ≥dwellWindow.
// This prevents a flapping gateway (handshake then drop) from resetting the
// backoff via a single brief success. A slot still stuck on the yfinance fallback
// (or disconnected) grows the backoff exponentially so the health checker stops
// re-probing IBKR every cycle. The slot must be exclusively owned (drained from
// avail) when this is called. #171 + #176
func (p *brokerPool) trackReconnectOutcome(conn *pooledConn) {
	if conn.b != nil && conn.isConnected() && !isOnFallback(conn) {
		conn.connectedSince = p.now()
		return
	}
	conn.connectedSince = time.Time{}
	conn.failCount++
	conn.nextRetry = p.now().Add(backoffFor(conn.failCount))
}

// clearBackoffIfStable relaxes a slot's backoff once it has held a live IBKR
// connection for ≥dwellWindow. Called from checkIdleConnections on healthy slots
// (the no-op-reconnect path) so a stably-recovered gateway eventually clears the
// backoff growth left behind by a previous failure storm. Also serves as the
// initial-observation hook: if the slot is live on IBKR but connectedSince is
// zero (e.g. acquire reconnected and we never recorded the moment), this stamps
// it now so the dwell timer can run. #176
func (p *brokerPool) clearBackoffIfStable(conn *pooledConn) {
	if conn.b == nil || isOnFallback(conn) {
		conn.connectedSince = time.Time{}
		return
	}
	if conn.connectedSince.IsZero() {
		conn.connectedSince = p.now()
		return
	}
	if p.now().Sub(conn.connectedSince) >= dwellWindow {
		conn.failCount = 0
		conn.nextRetry = time.Time{}
	}
}

// isOnFallback reports whether the slot's broker is currently serving the
// degraded (yfinance) fallback source. Returns false for nil brokers and for
// brokers that don't implement fallbackReporter.
func isOnFallback(conn *pooledConn) bool {
	if fr, ok := conn.b.(fallbackReporter); ok {
		return fr.UsingFallback()
	}
	return false
}

// defaultBrokerFactory returns the production brokerFactory that dials IBKR
// (with yfinance fallback) and returns an already-connected broker.
func defaultBrokerFactory(ibHost string, ibPort int, pythonBin string) brokerFactory {
	return func(ctx context.Context, clientID int64) (broker.Broker, error) {
		b := factory.NewWithFallback(ibkr.Config{
			Host:     ibHost,
			Port:     ibPort,
			ClientID: clientID,
		}, pythonBin)
		if err := b.Connect(ctx); err != nil {
			return nil, err
		}
		return b, nil
	}
}

// acquire blocks until a healthy connection is available or ctx is cancelled.
// The returned slot is exclusively owned by the caller until release() is called.
func (p *brokerPool) acquire(ctx context.Context) (*pooledConn, error) {
	select {
	case conn := <-p.avail:
		if !conn.isConnected() {
			rCtx, rCancel := context.WithTimeout(ctx, reconnectTimeout)
			defer rCancel()
			if err := p.reconnect(rCtx, conn); err != nil {
				// Return conn so the pool stays at full capacity.
				p.avail <- conn
				return nil, fmt.Errorf("broker pool: %w", err)
			}
		}
		conn.lastUsed = time.Now()
		return conn, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("broker pool: no slot available: %w", ctx.Err())
	}
}

// release returns a connection to the pool.
//
// If healthy is true and the broker is still connected the slot goes straight
// back into the available queue (fast path — no reconnect overhead).
//
// If healthy is false or the broker has silently disconnected, the slot is
// reconnected asynchronously before being re-queued, so the next caller
// always receives a live connection.
func (p *brokerPool) release(conn *pooledConn, healthy bool) {
	if healthy && conn.isConnected() {
		p.avail <- conn
		return
	}
	// Unhealthy path: disconnect and reconnect in a goroutine, then re-queue.
	go func() {
		rCtx, rCancel := context.WithTimeout(p.ctx, reconnectTimeout)
		defer rCancel()
		if err := p.reconnect(rCtx, conn); err != nil {
			log.Printf("broker pool: async reconnect clientID %d failed: %v", conn.id, err)
		}
		// Always re-queue (even on failure) so the pool stays at full capacity.
		// The next acquire will detect IsConnected()==false and retry.
		select {
		case p.avail <- conn:
		case <-p.ctx.Done():
		}
	}()
}

// reconnect disconnects any existing broker and creates a new one via factory.
// It is safe to call concurrently with acquire/release because the slot is
// never in the avail channel while reconnect runs.
func (p *brokerPool) reconnect(ctx context.Context, conn *pooledConn) error {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.b != nil {
		_ = conn.b.Disconnect()
		conn.b = nil
	}
	b, err := p.factory(ctx, conn.id)
	if err != nil {
		return fmt.Errorf("reconnect clientID %d: %w", conn.id, err)
	}
	conn.b = b
	conn.lastUsed = time.Now()
	return nil
}

// close stops the background health-checker and disconnects all connections.
// Call this when the server shuts down.
func (p *brokerPool) close() {
	p.cancel()
	for _, c := range p.conns {
		c.mu.Lock()
		if c.b != nil {
			_ = c.b.Disconnect()
			c.b = nil
		}
		c.mu.Unlock()
	}
}

// cap returns the total pool capacity.
func (p *brokerPool) cap() int { return len(p.conns) }

// available returns the number of currently idle slots (approximate; changes
// concurrently as slots are acquired and released).
func (p *brokerPool) available() int { return len(p.avail) }

// healthChecker runs a periodic probe of all idle connections and reconnects
// any that have silently dropped (e.g., TWS restarted, TCP keepalive timeout).
func (p *brokerPool) healthChecker() {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.checkIdleConnections()
		}
	}
}

// checkIdleConnections drains all currently idle slots, health-checks each
// one, reconnects any that are dead, then returns them all to the pool.
// Slots that are in-use (acquired by callers) are not touched.
//
// Health check priority:
//  1. If the broker implements broker.Pinger, use an active round-trip probe
//     (ReqCurrentTime for IBKR) — catches TCP zombies that IsConnected() misses.
//  2. Otherwise fall back to IsConnected() (catches ConnectionClosed callbacks).
//
// Additionally, slots running on yfinance fallback are always reconnected so
// the pool automatically switches back to IBKR when TWS recovers.
func (p *brokerPool) checkIdleConnections() {
	n := len(p.avail)
	checked := make([]*pooledConn, 0, n)

	// Non-blocking drain — only grab what's currently idle.
	for i := 0; i < n; i++ {
		select {
		case conn := <-p.avail:
			checked = append(checked, conn)
		default:
		}
	}

	for _, conn := range checked {
		unhealthy := p.isUnhealthy(conn)
		needsReconnect := false

		if unhealthy {
			// Genuine-unhealthy path: gate by nextRetry so a flapping slot is
			// re-probed on the backoff schedule rather than every cycle.
			// acquire's on-demand reconnect remains ungated so user-facing
			// requests still get a connection immediately. #176
			if !p.now().Before(conn.nextRetry) {
				needsReconnect = true
			}
		} else {
			// Healthy path: relax backoff if the connection has dwelled long
			// enough, then check for an IBKR switchback if we're stuck on
			// fallback. Without the switchback gate, an unreachable IBKR makes
			// every idle fallback slot reconnect every 30s cycle, hammering
			// TWS's 32-connection limit and driving the racy ibapi
			// connect/teardown path. #171
			p.clearBackoffIfStable(conn)
			if isOnFallback(conn) && !p.now().Before(conn.nextRetry) {
				needsReconnect = true
				log.Printf("broker pool: clientID %d on yfinance — attempting IBKR switchback", conn.id)
			}
		}
		if needsReconnect {
			rCtx, rCancel := context.WithTimeout(p.ctx, reconnectTimeout)
			if err := p.reconnect(rCtx, conn); err != nil {
				log.Printf("broker pool: health check reconnect clientID %d: %v", conn.id, err)
			}
			rCancel()
			p.trackReconnectOutcome(conn)
		}
		select {
		case p.avail <- conn:
		case <-p.ctx.Done():
			return
		}
	}
}

// isUnhealthy returns true if the slot's connection needs to be replaced.
// Uses an active Ping probe when the broker supports it (IBKR); falls back to
// IsConnected() for brokers that don't implement broker.Pinger (yfinance).
func (p *brokerPool) isUnhealthy(conn *pooledConn) bool {
	if conn.b == nil {
		return true
	}
	if pinger, ok := conn.b.(broker.Pinger); ok {
		pingCtx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
		defer cancel()
		if err := pinger.Ping(pingCtx); err != nil {
			log.Printf("broker pool: ping failed clientID %d: %v", conn.id, err)
			return true
		}
		return false
	}
	return !conn.isConnected()
}
