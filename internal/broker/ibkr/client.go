package ibkr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/intelshared"
	"github.com/IS908/optix/pkg/model"
	"github.com/rs/zerolog"
	"github.com/scmhub/ibapi"
)

// Config holds IB Gateway/TWS connection settings.
type Config struct {
	Host     string
	Port     int
	ClientID int64
}

const (
	ibClientIDInUseCode int64 = 326

	ibHandshakeTimeout = 10 * time.Second

	clientIDFallbackBase   int64 = 100000
	clientIDPIDModulo      int64 = 10000
	clientIDPrimaryModulo  int64 = 100000
	clientIDFallbackStride int64 = clientIDPrimaryModulo * 2
)

var errIBKRHandshakeTimeout = errors.New("timeout waiting for NextValidID (TCP connected but handshake did not complete)")

type ibConnectError struct {
	clientID  int64
	retryable bool
	err       error
}

type clientIDAttemptHooks struct {
	reset   func()
	connect func(context.Context, int64) error
	wait    func(context.Context, int) error
}

type clientIDAttemptResult struct {
	clientID  int64
	connected bool
	errors    []error
}

func (e *ibConnectError) Error() string {
	return fmt.Sprintf("IB Gateway/TWS handshake failed for clientID %d: %v", e.clientID, e.err)
}

func (e *ibConnectError) Unwrap() error {
	return e.err
}

// Client wraps the IB Gateway/TWS API connection.
//
// Contract: single-connect. A Client instance is meant to run exactly one
// Connect → use → Disconnect lifecycle. Connect replaces c.wrapper and
// c.ibClient under c.mu (resetAPIClientLocked, one fresh pair per attempt —
// see #171/#189), but every request path (GetQuote, Ping, watchDisconnect,
// ...) reads those two fields without taking c.mu — safe today only because
// every caller in this codebase (broker_pool's factory, the CLI's ad hoc
// connects) constructs a brand-new Client per Connect() rather than
// reconnecting an existing one (#192 finding 3). Reusing a Client across
// more than one Connect/Disconnect cycle is undefined behavior — a request
// goroutine racing a concurrent Connect-after-Disconnect on the same
// instance is a real Go data race on c.wrapper/c.ibClient. If that pattern
// is ever needed, add locking to the read paths first.
type Client struct {
	cfg      Config
	wrapper  *IbWrapper
	ibClient *ibapi.EClient

	// handshakeTimeout bounds how long a single connect attempt waits for
	// NextValidID once the TCP/IB-protocol handshake in connectOnceLocked
	// has returned. Defaults to ibHandshakeTimeout; Connect() may shrink it
	// per attempt via perAttemptHandshakeTimeout when the caller's ctx
	// carries a deadline (#192 fix (a)). Tests override this field directly
	// to keep wedge/timeout scenarios fast.
	handshakeTimeout time.Duration

	mu            sync.RWMutex
	connected     bool
	disconnecting bool
	watchCancel   context.CancelFunc // stops the disconnect-watcher goroutine; nil if not running

	reqIDCounter int64 // atomic counter for request IDs
}

func init() {
	configureIBAPILogger()
}

func configureIBAPILogger() {
	ibapi.SetLogger(zerolog.New(io.Discard))
}

// New creates a new IB Gateway/TWS client.
func New(cfg Config) *Client {
	wrapper := newIbWrapper()
	ibClient := ibapi.NewEClient(wrapper)
	return &Client{
		cfg:              cfg,
		wrapper:          wrapper,
		ibClient:         ibClient,
		handshakeTimeout: ibHandshakeTimeout,
	}
}

// Connect establishes a connection to IB Gateway or TWS.
//
// Single-connect: see the Client doc comment. Calling Connect a second time
// on an instance that already completed a Connect/Disconnect cycle is
// unsupported — construct a new Client instead.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	if c.watchCancel != nil {
		c.watchCancel()
		c.watchCancel = nil
	}

	primaryClientID := c.cfg.ClientID
	candidates := clientIDCandidates(primaryClientID, os.Getpid())
	attempt := 0
	result, err := runClientIDAttempts(ctx, candidates, clientIDAttemptHooks{
		reset: c.resetAPIClientLocked,
		connect: func(ctx context.Context, clientID int64) error {
			c.cfg.ClientID = clientID
			// #192 fix (a): derive this attempt's handshake wait from the
			// ctx's remaining deadline (if any), split across the attempts
			// that could still run. Only matters on the retry path (326
			// clientID-in-use, see fix (b) below) — the last attempt always
			// gets whatever budget remains.
			remaining := len(candidates) - attempt
			attempt++
			timeout := perAttemptHandshakeTimeout(ctx, remaining, c.handshakeTimeout)
			return c.connectOnceLocked(ctx, clientID, timeout)
		},
		wait: waitBeforeConnectRetry,
	})
	if err != nil {
		c.cfg.ClientID = primaryClientID
		return err
	}
	if !result.connected {
		c.cfg.ClientID = primaryClientID
		return connectAttemptsError(c.cfg.Host, c.cfg.Port, candidates[:len(result.errors)], result.errors)
	}

	if result.clientID != primaryClientID {
		log.Printf("ibkr: connected with fallback clientID %d after primary clientID %d failed", result.clientID, primaryClientID)
	}
	// c.cfg.ClientID intentionally stays at result.clientID (the fallback
	// candidate that actually connected) rather than being restored to
	// primaryClientID. Per the single-connect contract above, this Client
	// instance never attempts another Connect(), so the "ID drift" a reused
	// instance would see on its next round is moot (#192 finding 5).
	c.finishConnectLocked()
	return nil
}

func (c *Client) resetAPIClientLocked() {
	wrapper := newIbWrapper()
	c.wrapper = wrapper
	c.ibClient = ibapi.NewEClient(wrapper)
}

func (c *Client) connectOnceLocked(ctx context.Context, clientID int64, handshakeTimeout time.Duration) error {
	err := c.ibClient.Connect(c.cfg.Host, c.cfg.Port, clientID)
	if err != nil {
		return fmt.Errorf("connect to IB Gateway/TWS at %s:%d with clientID %d: %w", c.cfg.Host, c.cfg.Port, clientID, err)
	}
	return c.awaitHandshakeLocked(ctx, clientID, handshakeTimeout)
}

// awaitHandshakeLocked waits for NextValidID (which signals the post-TCP-
// connect IB API handshake is complete), bounded by handshakeTimeout and
// ctx. Split out from connectOnceLocked so the retry/timeout classification
// below is unit-testable directly against wrapper channels, without a real
// TCP dial to IB Gateway/TWS (#192).
func (c *Client) awaitHandshakeLocked(ctx context.Context, clientID int64, handshakeTimeout time.Duration) error {
	timer := time.NewTimer(handshakeTimeout)
	defer timer.Stop()

	select {
	case firstID := <-c.wrapper.nextValidID:
		atomic.StoreInt64(&c.reqIDCounter, firstID)
		return nil
	case err := <-c.wrapper.connectErrors:
		if dErr := c.ibClient.Disconnect(); dErr != nil {
			log.Printf("ibkr: disconnect after handshake error (clientID %d): %v", clientID, dErr)
		}
		return &ibConnectError{clientID: clientID, retryable: isRetryableConnectCause(err), err: err}
	case <-timer.C:
		if dErr := c.ibClient.Disconnect(); dErr != nil {
			log.Printf("ibkr: disconnect after handshake timeout (clientID %d): %v", clientID, dErr)
		}
		// #192 fix (b): a handshake timeout means TCP connected but
		// NextValidID never arrived — the classic wedged-gateway symptom
		// (#188/#189). Retrying just repeats the same wedge and burns
		// another full handshake window; NOT retryable keeps one Connect()
		// call to ~one handshake window so callers with a bounded ctx (the
		// web UI's broker pool, reconnectTimeout=15s) still have budget left
		// for FallbackBroker's #41 guardrail to see ctx.Err()==nil and fall
		// through to yfinance. Only error 326 (clientID already in use,
		// isRetryableConnectCause below) is worth retrying — it resolves
		// near-instantly against a fresh candidate ID.
		return &ibConnectError{clientID: clientID, retryable: false, err: errIBKRHandshakeTimeout}
	case <-ctx.Done():
		if dErr := c.ibClient.Disconnect(); dErr != nil {
			log.Printf("ibkr: disconnect after context cancel (clientID %d): %v", clientID, dErr)
		}
		return ctx.Err()
	}
}

// perAttemptHandshakeTimeout derives the handshake wait for a single connect
// attempt (#192 fix (a)). When ctx carries a deadline, the remaining budget
// is split evenly across the attempts that could still run — this attempt
// plus attemptsRemaining-1 further retries — so a chain of retryable
// failures (error 326, clientID already in use) can't let one attempt eat
// the whole budget and starve the rest. The result is also capped at def so
// a distant deadline never grants a single attempt more than the configured
// default. Contexts without a deadline (e.g. the CLI's context.Background())
// fall back to def unchanged — this only changes behavior for callers that
// already impose a bound, like the web UI broker pool's reconnectTimeout.
func perAttemptHandshakeTimeout(ctx context.Context, attemptsRemaining int, def time.Duration) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return def
	}
	if attemptsRemaining < 1 {
		attemptsRemaining = 1
	}
	budget := time.Until(deadline) / time.Duration(attemptsRemaining)
	if budget <= 0 {
		return 0
	}
	if budget > def {
		return def
	}
	return budget
}

func (c *Client) finishConnectLocked() {
	// Use delayed market data (type 3) so the client works without
	// a real-time API subscription.  Accounts that do have live
	// subscriptions will still receive live data for those symbols;
	// for everything else IB falls back to the 15-min delayed feed.
	c.ibClient.ReqMarketDataType(3)

	c.connected = true
	c.disconnecting = false

	// No disconnectCh drain here (#192 finding 2/4): resetAPIClientLocked
	// gives every connect attempt a fresh wrapper (#189), so anything
	// sitting in this session's disconnectCh by the time we reach here is a
	// genuine TCP drop that happened between NextValidID and this call — not
	// leftover noise from a prior session sharing the same wrapper. Draining
	// it would leave connected=true on a dead socket until the next 30s Ping
	// cycle catches it; leaving it in place lets watchDisconnect below
	// observe it immediately.
	// Start a goroutine that watches for TCP-drop callbacks from ibapi and
	// immediately marks the client as disconnected without waiting for the
	// next health check.
	watchCtx, watchCancel := context.WithCancel(context.Background())
	if c.watchCancel != nil {
		c.watchCancel() // stop previous watcher if any
	}
	c.watchCancel = watchCancel
	go c.watchDisconnect(watchCtx)
}

func clientIDCandidates(primary int64, pid int) []int64 {
	if primary == 0 {
		return []int64{0}
	}
	pidPart := int64(pid) % clientIDPIDModulo
	if pidPart < 0 {
		pidPart = -pidPart
	}
	primaryPart := primary % clientIDPrimaryModulo
	if primaryPart < 0 {
		primaryPart = -primaryPart
	}
	// Pair a process bucket with the full five-digit primary-ID bucket. The
	// result stays below MaxInt32 while separating the server's 7200/8700
	// dynamic ranges and adjacent short-lived CLI processes.
	firstFallback := clientIDFallbackBase + pidPart*clientIDFallbackStride + primaryPart*2
	candidates := []int64{primary}
	for fallback := firstFallback; len(candidates) < 3; fallback++ {
		if fallback != primary {
			candidates = append(candidates, fallback)
		}
	}
	return candidates
}

func runClientIDAttempts(ctx context.Context, candidates []int64, hooks clientIDAttemptHooks) (clientIDAttemptResult, error) {
	result := clientIDAttemptResult{errors: make([]error, 0, len(candidates))}
	for attempt, clientID := range candidates {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		hooks.reset()
		err := hooks.connect(ctx, clientID)
		if err == nil {
			result.clientID = clientID
			result.connected = true
			return result, nil
		}

		result.errors = append(result.errors, err)
		if !isRetryableConnectError(err) || attempt == len(candidates)-1 {
			return result, nil
		}
		if err := hooks.wait(ctx, attempt); err != nil {
			return result, err
		}
	}
	return result, nil
}

// isRetryableConnectCause classifies an error delivered via
// c.wrapper.connectErrors (a genuine IB API error during the handshake wait,
// as opposed to a local handshake timeout — see #192 fix (b) in
// awaitHandshakeLocked, which never routes through here). Only error 326
// (clientID already in use) is worth retrying: it resolves near-instantly
// against a fresh candidate ID, unlike a wedged gateway where retrying just
// repeats the same timeout.
func isRetryableConnectCause(err error) bool {
	var ibErr *ibAPIError
	if errors.As(err, &ibErr) {
		return ibErr.code == ibClientIDInUseCode
	}
	return false
}

func isRetryableConnectError(err error) bool {
	var connectErr *ibConnectError
	return errors.As(err, &connectErr) && connectErr.retryable
}

func waitBeforeConnectRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(100+attempt*150) * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func connectAttemptsError(host string, port int, candidates []int64, errs []error) error {
	if len(errs) == 0 {
		return fmt.Errorf("connect to IB Gateway/TWS at %s:%d failed before attempting client IDs %v", host, port, candidates)
	}
	if len(errs) == 1 {
		if len(candidates) == 1 {
			return fmt.Errorf("connect to IB Gateway/TWS at %s:%d failed with client ID %d: %w", host, port, candidates[0], errs[0])
		}
		return fmt.Errorf("connect to IB Gateway/TWS at %s:%d failed: %w", host, port, errs[0])
	}
	parts := make([]string, len(errs))
	for i, err := range errs {
		parts[i] = err.Error()
	}
	return fmt.Errorf("connect to IB Gateway/TWS at %s:%d failed after client IDs %v: %s", host, port, candidates, strings.Join(parts, "; "))
}

// Disconnect closes the IB Gateway/TWS connection.
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}
	c.disconnecting = true
	// Stop the watcher goroutine so it doesn't fire after an intentional disconnect.
	if c.watchCancel != nil {
		c.watchCancel()
		c.watchCancel = nil
	}
	err := c.ibClient.Disconnect()
	c.connected = false
	select {
	case <-c.wrapper.disconnectCh:
	default:
	}
	return err
}

// watchDisconnect runs in a goroutine while the client is connected. When
// ibapi fires ConnectionClosed() (TCP drop), it sets connected=false so the
// broker pool can detect the dead slot without waiting for the health ticker.
func (c *Client) watchDisconnect(ctx context.Context) {
	select {
	case <-c.wrapper.disconnectCh:
		c.mu.Lock()
		expected := c.disconnecting
		c.connected = false
		c.mu.Unlock()
		if !expected {
			log.Printf("ibkr: TCP connection dropped (clientID %d) — slot marked unhealthy", c.cfg.ClientID)
		}
	case <-ctx.Done():
	}
}

// Ping sends a lightweight ReqCurrentTime round-trip to verify the connection
// is alive. Returns an error if TWS does not respond within the context deadline.
// Implements broker.Pinger.
func (c *Client) Ping(ctx context.Context) error {
	if !c.IsConnected() {
		return fmt.Errorf("ibkr: not connected (clientID %d)", c.cfg.ClientID)
	}
	// Drain any stale response from a prior ping.
	select {
	case <-c.wrapper.pingCh:
	default:
	}
	c.ibClient.ReqCurrentTime()
	select {
	case <-c.wrapper.pingCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("ibkr: ping timeout (clientID %d): %w", c.cfg.ClientID, ctx.Err())
	}
}

// IsConnected returns the connection status.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// nextReqID returns a new monotonically increasing request ID.
func (c *Client) nextReqID() int64 {
	return atomic.AddInt64(&c.reqIDCounter, 1)
}

// stockContract builds a basic US equity contract.
func stockContract(symbol string) *ibapi.Contract {
	return &ibapi.Contract{
		Symbol:   symbol,
		SecType:  "STK",
		Exchange: "SMART",
		Currency: "USD",
	}
}

// optionContract builds an OPT contract for ReqMktData. `right` is "C" or "P",
// expiration is "YYYYMMDD", strike is the option strike price.
func optionContract(symbol, expiration, right string, strike float64) *ibapi.Contract {
	return &ibapi.Contract{
		Symbol:                       symbol,
		SecType:                      "OPT",
		Exchange:                     "SMART",
		Currency:                     "USD",
		LastTradeDateOrContractMonth: expiration,
		Strike:                       strike,
		Right:                        right,
		Multiplier:                   "100",
	}
}

// GetQuote retrieves the latest stock quote from IB.
//
// Uses streaming market data with a short collection window so it works with
// both snapshot-capable (live) and non-snapshot (delayed) data types.
// During pre-market and post-market sessions, requests outside-RTH ticks so
// the quote reflects the latest extended-hours price, not just the prior close.
func (c *Client) GetQuote(ctx context.Context, symbol string) (*model.StockQuote, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("not connected to IB Gateway/TWS")
	}

	session := model.USMarketSession(time.Now())

	reqID := c.nextReqID()
	pq := c.wrapper.registerQuote(reqID, "") // "" — stock quote, no option side
	errCh := c.wrapper.registerError(reqID)
	defer c.wrapper.unregister(reqID)

	// Request generic tick 221 (MARK_PRICE) during extended hours — this
	// provides the mark price that IB calculates from the best bid/ask even
	// when no LAST trade has occurred in the extended session.
	genericTicks := ""
	if session.IsExtendedHours() {
		genericTicks = "221"
	}

	// snapshot=false → streaming; we cancel after TickSnapshotEnd fires OR
	// after a short timeout, whichever comes first.  This works for both live
	// and delayed (type 3) data where snapshot permissions may be 0.
	c.ibClient.ReqMktData(reqID, stockContract(symbol), genericTicks, false, false, nil)

	// Give IB up to 5 s to deliver the initial ticks.
	tickCtx, tickCancel := context.WithTimeout(ctx, 5*time.Second)
	defer tickCancel()

	select {
	case <-pq.done: // TickSnapshotEnd fired (live data with snapshot perms)
	case <-tickCtx.Done(): // timeout: use whatever ticks arrived so far
	case err := <-errCh:
		c.ibClient.CancelMktData(reqID)
		// 10089 / 10090 = no API market data subscription.
		// Fall back to the last historical daily close so the tool works
		// without a paid market data subscription.
		if isNoSubscriptionErr(err) {
			q, hErr := c.quoteFromHistory(ctx, symbol)
			if hErr != nil {
				return nil, hErr
			}
			q.MarketSession = session
			return q, nil
		}
		return nil, fmt.Errorf("GetQuote %s: %w", symbol, err)
	}

	// Always cancel the stream so TWS stops sending updates.
	c.ibClient.CancelMktData(reqID)

	quote := pq.snapshot()
	last := quoteMark(quote.mark, quote.last, quote.bid, quote.ask, quote.close)
	change, changePct := quoteChange(last, quote.close)

	// If streaming yielded no price data at all, fall back to historical close.
	if last == 0 {
		q, err := c.quoteFromHistory(ctx, symbol)
		if err != nil {
			return nil, err
		}
		q.MarketSession = session
		return q, nil
	}

	return &model.StockQuote{
		Symbol:        symbol,
		Last:          last,
		Bid:           quote.bid,
		Ask:           quote.ask,
		Change:        change,
		ChangePct:     changePct,
		Close:         quote.close,
		Volume:        int64(quote.volume),
		Timestamp:     time.Now(),
		MarketSession: session,
	}, nil
}

// GetMarketDepth retrieves a bounded SMART market-depth snapshot for a US
// equity/ETF symbol. It returns partial rows if IB delivered some depth before
// the collection timeout; callers should treat absence of rows as degraded data.
func (c *Client) GetMarketDepth(ctx context.Context, symbol string, levels int) (*model.MarketDepth, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("not connected to IB Gateway/TWS")
	}
	if levels <= 0 {
		levels = 5
	}

	reqID := c.nextReqID()
	pd := c.wrapper.registerDepth(reqID, levels)
	errCh := c.wrapper.registerError(reqID)
	defer c.wrapper.unregister(reqID)

	const smartDepth = true
	c.ibClient.ReqMktDepth(reqID, stockContract(symbol), levels, smartDepth, nil)
	defer c.ibClient.CancelMktDepth(reqID, smartDepth)

	depthCtx, cancel := context.WithTimeout(ctx, marketDepthTimeout)
	defer cancel()

	select {
	case <-pd.done:
	case err := <-errCh:
		return nil, fmt.Errorf("GetMarketDepth %s: %w", symbol, err)
	case <-depthCtx.Done():
	}

	snapshot := pd.snapshot()
	if len(snapshot) == 0 {
		if err := depthCtx.Err(); err != nil {
			return nil, fmt.Errorf("GetMarketDepth %s: no depth rows before timeout: %w", symbol, err)
		}
		return nil, fmt.Errorf("GetMarketDepth %s: no depth rows", symbol)
	}
	return &model.MarketDepth{
		Symbol:    symbol,
		Timestamp: time.Now().UTC(),
		Levels:    snapshot,
	}, nil
}

// GetHistoricalBars retrieves historical OHLCV data from IB.
//
// timeframe examples: "1 day", "1 hour", "5 mins"
// startDate / endDate: "20240101 00:00:00 US/Eastern" or ""
//
// For daily bars, useRTH is always true (standard OHLCV).
// For intraday bars (< 1 day), useRTH is false during extended-hours sessions
// so that pre-market and after-hours bars are included.
func (c *Client) GetHistoricalBars(ctx context.Context, symbol, timeframe, startDate, endDate string) ([]model.OHLCV, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("not connected to IB Gateway/TWS")
	}

	reqID := c.nextReqID()
	pb := c.wrapper.registerBars(reqID)
	errCh := c.wrapper.registerError(reqID)
	defer c.wrapper.unregister(reqID)

	duration := historicalDuration(startDate, endDate, time.Now())

	// For daily bars, always use RTH to get standard OHLCV.
	// For intraday bars during extended hours, include outside-RTH data
	// so pre-market (04:00-09:30) and post-market (16:00-20:00) bars appear.
	useRTH := true
	if timeframe != "1 day" {
		session := model.USMarketSession(time.Now())
		if session.IsExtendedHours() {
			useRTH = false
		}
	}

	c.ibClient.ReqHistoricalData(
		reqID, stockContract(symbol),
		endDate,   // endDateTime ("" = now)
		duration,  // durationStr
		timeframe, // barSizeSetting e.g. "1 day"
		"TRADES",  // whatToShow
		useRTH,    // useRTH: false during extended hours for intraday bars
		1,         // formatDate (1 = yyyymmdd hh:mm:ss)
		false,     // keepUpToDate
		nil,       // chartOptions
	)

	select {
	case <-pb.done:
	case err := <-errCh:
		return nil, fmt.Errorf("GetHistoricalBars %s: %w", symbol, err)
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	bars := make([]model.OHLCV, 0, len(pb.bars))
	for _, b := range pb.bars {
		t, _ := parseIBDate(b.Date)
		bars = append(bars, model.OHLCV{
			Timestamp: t,
			Open:      b.Open,
			High:      b.High,
			Low:       b.Low,
			Close:     b.Close,
			Volume:    int64(b.Volume.Float()),
		})
	}
	return bars, nil
}

// getConID resolves the IB contract ID for a US stock symbol.
// The ProtoBuf path of ReqSecDefOptParams (server v212+) requires a non-zero conID.
func (c *Client) getConID(ctx context.Context, symbol string) (int64, error) {
	reqID := c.nextReqID()
	pcd := c.wrapper.registerContractDetails(reqID)
	errCh := c.wrapper.registerError(reqID)
	defer c.wrapper.unregister(reqID)

	c.ibClient.ReqContractDetails(reqID, stockContract(symbol))

	select {
	case <-pcd.done:
	case err := <-errCh:
		return 0, fmt.Errorf("getConID %s: %w", symbol, err)
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	if pcd.conID == 0 {
		return 0, fmt.Errorf("getConID %s: no contract found", symbol)
	}
	return pcd.conID, nil
}

// GetOptionChain retrieves the option chain for an underlying.
//
// expiration: "YYYYMMDD" to filter, or "" for all near-term expirations.
func (c *Client) GetOptionChain(ctx context.Context, underlying string, expiration string) (*model.OptionChain, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("not connected to IB Gateway/TWS")
	}

	// Step 1a: resolve the contract ID (required by protobuf-path servers v212+).
	conID, err := c.getConID(ctx, underlying)
	if err != nil {
		return nil, fmt.Errorf("GetOptionChain %s: %w", underlying, err)
	}

	// Step 1b: get option parameters (expirations + strikes).
	reqID := c.nextReqID()
	pp := c.wrapper.registerOptParams(reqID)
	errCh := c.wrapper.registerError(reqID)
	defer c.wrapper.unregister(reqID)

	c.ibClient.ReqSecDefOptParams(reqID, underlying, "", "STK", conID)

	select {
	case <-pp.done:
	case err := <-errCh:
		return nil, fmt.Errorf("GetOptionChain %s params: %w", underlying, err)
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if len(pp.expirations) == 0 {
		return nil, fmt.Errorf("no option expirations found for %s", underlying)
	}

	// Step 2: build the chain structure from SecDefOptParams data.
	// We do NOT request live market data per-contract (requires subscription).
	// Pricing / Greeks / IV are computed by the Python analysis engine using
	// Black-Scholes with the current underlying price and historical volatility.

	// Filter and sort expirations.
	sort.Strings(pp.expirations)
	expirations := pp.expirations
	// When the caller specifies an expiration, we need the full list so a
	// miss can produce a meaningful ErrExpiryNotAvailable. Without an explicit
	// expiration, truncate to the 4 nearest (sufficient for ~2-week analysis
	// and matches the historical behaviour).
	if expiration == "" && len(expirations) > 4 {
		expirations = expirations[:4]
	}

	sort.Float64s(pp.strikes)

	chain := &model.OptionChain{Underlying: underlying}

	for _, exp := range expirations {
		expiry := model.OptionChainExpiry{Expiration: exp}
		for _, strike := range pp.strikes {
			expiry.Calls = append(expiry.Calls, model.OptionQuote{
				Underlying: underlying,
				Expiration: exp,
				Strike:     strike,
				OptionType: model.OptionTypeCall,
			})
			expiry.Puts = append(expiry.Puts, model.OptionQuote{
				Underlying: underlying,
				Expiration: exp,
				Strike:     strike,
				OptionType: model.OptionTypePut,
			})
		}
		chain.Expirations = append(chain.Expirations, expiry)
	}

	// When the caller specified an expiration, narrow the chain to that single
	// expiry. This preserves the historical GetOptionChain(symbol, expiry)
	// contract (1-element chain) and surfaces *broker.ErrExpiryNotAvailable on
	// a miss, mirroring the behaviour of GetOptionChainWithOI.
	if expiration != "" {
		matched, err := pickRequestedExpiry(chain, expiration)
		if err != nil {
			return nil, err
		}
		chain.Expirations = []model.OptionChainExpiry{*matched}
	}

	return chain, nil
}

// GetOptionChainWithOI returns the option chain enriched with per-contract
// Open Interest values. To bound network round-trips and respect IB pacing
// limits (50 msg/sec), it limits the request scope:
//
//  1. Only the nearest expiration is fetched (or `expiration` if specified)
//  2. Only strikes within ±oiStrikeWindowPct of the current spot price
//  3. Concurrent reqMktData calls are bounded by oiMaxConcurrent
//  4. Each per-contract request times out after oiPerContractTimeout
//
// Implementation note: IB delivers Open Interest only via streaming market data
// with generic-tick-list "101" (snapshots don't include generic ticks). We start
// the stream, wait for the first OPEN_INTEREST tick, then immediately
// cancelMktData to avoid leaking subscriptions.
func (c *Client) GetOptionChainWithOI(ctx context.Context, underlying string, expiration string) (*model.OptionChain, error) {
	// Reuse the structure-only chain to get strikes/expirations.
	chain, err := c.GetOptionChain(ctx, underlying, expiration)
	if err != nil {
		return nil, err
	}
	if chain == nil || len(chain.Expirations) == 0 {
		return chain, nil
	}

	// Get spot price to filter ATM strikes (±15% window).
	quote, qErr := c.GetQuote(ctx, underlying)
	spot, authoritativeSpot := spotForOIWindow(chain, quote)
	if spot <= 0 {
		return chain, nil
	}
	if authoritativeSpot {
		chain.UnderlyingPrice = spot
	} else {
		log.Printf("ibkr: GetOptionChainWithOI %s: spot quote unavailable (%v) — using median strike for OI window only", underlying, qErr)
	}
	low := spot * (1 - oiStrikeWindowPct)
	high := spot * (1 + oiStrikeWindowPct)

	// Pick the requested expiry (or the nearest when none specified). If the
	// caller asked for an expiry IBKR doesn't offer, pickRequestedExpiry
	// returns *broker.ErrExpiryNotAvailable with the full available list.
	exp, perr := pickRequestedExpiry(chain, expiration)
	if perr != nil {
		return nil, perr
	}

	// Build the worklist: every (strike, right) pair within the window.
	type job struct {
		strike float64
		right  string // "C" or "P"
		idx    int    // index in exp.Calls or exp.Puts
	}
	var jobs []job
	for i, c := range exp.Calls {
		if c.Strike >= low && c.Strike <= high {
			jobs = append(jobs, job{c.Strike, "C", i})
		}
	}
	for i, p := range exp.Puts {
		if p.Strike >= low && p.Strike <= high {
			jobs = append(jobs, job{p.Strike, "P", i})
		}
	}
	if len(jobs) == 0 {
		log.Printf("ibkr: GetOptionChainWithOI %s: no strikes in ±%.0f%% window of $%.2f", underlying, oiStrikeWindowPct*100, spot)
		return chain, nil
	}

	log.Printf("ibkr: fetching OI for %d contracts (%s exp=%s spot=$%.2f)", len(jobs), underlying, exp.Expiration, spot)

	// Concurrent OI fetch with bounded worker pool.
	sem := make(chan struct{}, oiMaxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex // guards exp.Calls / exp.Puts mutation

	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			oi, iv := c.fetchOIForContract(ctx, underlying, exp.Expiration, j.right, j.strike)

			mu.Lock()
			defer mu.Unlock()
			if j.right == "C" {
				if oi > 0 {
					exp.Calls[j.idx].OpenInterest = oi
				}
				if iv > 0 {
					exp.Calls[j.idx].ImpliedVolatility = iv
				}
			} else {
				if oi > 0 {
					exp.Puts[j.idx].OpenInterest = oi
				}
				if iv > 0 {
					exp.Puts[j.idx].ImpliedVolatility = iv
				}
			}
		}(j)
	}
	wg.Wait()

	return chain, nil
}

func spotForOIWindow(chain *model.OptionChain, quote *model.StockQuote) (float64, bool) {
	if quote != nil && quote.Last > 0 {
		return quote.Last, true
	}
	if chain == nil || len(chain.Expirations) == 0 || len(chain.Expirations[0].Calls) == 0 {
		return 0, false
	}

	strikes := chain.Expirations[0].Calls
	return strikes[len(strikes)/2].Strike, false
}

// fetchOIForContract issues a streaming reqMktData with generic tick 101 (Open
// Interest), waits for the first OI tick (or timeout), then cancels the stream.
// Returns (oi, iv). Either may be zero if no data arrived in time.
func (c *Client) fetchOIForContract(ctx context.Context, symbol, expiration, right string, strike float64) (int32, float64) {
	reqID := c.nextReqID()
	po := c.wrapper.registerOI(reqID)
	errCh := c.wrapper.registerError(reqID)
	defer c.wrapper.unregister(reqID)

	contract := optionContract(symbol, expiration, right, strike)

	// Generic tick "101" = "Option Open Interest" — required for OI on options.
	// snapshot=false because snapshots don't include generic ticks.
	c.ibClient.ReqMktData(reqID, contract, "101", false, false, nil)
	defer c.ibClient.CancelMktData(reqID)

	timer := time.NewTimer(oiPerContractTimeout)
	defer timer.Stop()

	select {
	case <-po.done:
		// OI tick arrived (or error closed it). Read fields after small delay
		// to allow IV tick to also be captured.
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
		}
		oi := po.snapshot()
		return oi.openInterest, oi.iv
	case err := <-errCh:
		// Per-contract error (e.g., contract not found) — non-fatal.
		log.Printf("ibkr: OI fetch %s %s%.0f exp=%s: %v", symbol, right, strike, expiration, err)
		return 0, 0
	case <-timer.C:
		// Timeout — likely no live market data subscription for this contract,
		// or low-volume contract with no OI updates. Move on.
		oi := po.snapshot()
		return oi.openInterest, oi.iv
	case <-ctx.Done():
		return 0, 0
	}
}

// OI fetch tuning constants.
const (
	oiStrikeWindowPct    = 0.15            // ±15% around spot
	oiMaxConcurrent      = 5               // bounded for IB pacing (50 msg/sec limit)
	oiPerContractTimeout = 5 * time.Second // give up if no OI tick in 5s
	marketDepthTimeout   = 3 * time.Second // collect initial SMART depth rows
	optionQuoteTimeout   = 5 * time.Second // collect single-contract option ticks
)

// isNoSubscriptionErr returns true for IB errors that indicate missing market
// data API subscription (10089, 10090).  These are recoverable via historical data.
func isNoSubscriptionErr(err error) bool {
	s := err.Error()
	return strings.Contains(s, "10089") || strings.Contains(s, "10090")
}

// quoteFromHistory builds a StockQuote from the most recent historical daily bar.
// Used as fallback when live/delayed market data is not subscribed.
func (c *Client) quoteFromHistory(ctx context.Context, symbol string) (*model.StockQuote, error) {
	bars, err := c.GetHistoricalBars(ctx, symbol, "1 day", "", "")
	if err != nil || len(bars) == 0 {
		return nil, fmt.Errorf("GetQuote %s: no market data subscription and historical fallback failed: %w", symbol, err)
	}
	q, err := historicalQuoteFromBars(symbol, bars)
	if err != nil {
		return nil, fmt.Errorf("GetQuote %s: historical fallback failed: %w", symbol, err)
	}
	return q, nil
}

func historicalQuoteFromBars(symbol string, bars []model.OHLCV) (*model.StockQuote, error) {
	if len(bars) == 0 {
		return nil, fmt.Errorf("no historical bars")
	}
	last := bars[len(bars)-1]
	previousClose := 0.0
	if len(bars) >= 2 {
		previousClose = bars[len(bars)-2].Close
	}
	change, changePct := quoteChange(last.Close, previousClose)
	// Reconstruct a best-effort quote from the last daily bar.
	return &model.StockQuote{
		Symbol:    symbol,
		Last:      last.Close,
		Change:    change,
		ChangePct: changePct,
		Close:     previousClose,
		Open:      last.Open,
		High:      last.High,
		Low:       last.Low,
		Volume:    last.Volume,
		Timestamp: last.Timestamp,
	}, nil
}

func quoteMark(mark, last, bid, ask, close float64) float64 {
	if mark > 0 {
		return mark
	}
	if last > 0 {
		return last
	}
	if bid > 0 && ask > 0 {
		return (bid + ask) / 2
	}
	if close > 0 {
		return close
	}
	return 0
}

func quoteChange(last, close float64) (float64, float64) {
	if last <= 0 || close <= 0 {
		return 0, 0
	}
	change := last - close
	return change, change / close * 100
}

// pickRequestedExpiry returns the OptionChainExpiry matching `requested` (a
// YYYYMMDD string). When requested is empty, returns the first expiration in
// the chain (current "nearest" semantics). When requested doesn't match any
// expiration, returns *broker.ErrExpiryNotAvailable with the full available
// list so callers can render a helpful suggestion.
func pickRequestedExpiry(chain *model.OptionChain, requested string) (*model.OptionChainExpiry, error) {
	if len(chain.Expirations) == 0 {
		return nil, fmt.Errorf("no expirations returned for %s", chain.Underlying)
	}
	if requested == "" {
		return &chain.Expirations[0], nil
	}
	for i := range chain.Expirations {
		if chain.Expirations[i].Expiration == requested {
			return &chain.Expirations[i], nil
		}
	}
	avail := make([]string, 0, len(chain.Expirations))
	for _, e := range chain.Expirations {
		avail = append(avail, e.Expiration)
	}
	return nil, &broker.ErrExpiryNotAvailable{
		Underlying: chain.Underlying,
		Requested:  requested,
		Available:  avail,
	}
}

// parseIBDate parses IB historical bar date strings. Observed formats:
//   - "20240101" — daily+ bar sizes.
//   - "20240101 09:30:00" — older/documented intraday form, no zone.
//   - "20240101 09:30:00 US/Eastern" — what IB actually sends for
//     sub-daily bar sizes (confirmed live against IB Gateway during #191
//     verification). The trailing zone token names the zone the leading
//     date/time fields are *already expressed in* — it's NY wall-clock
//     (IB formatDate=1), not a UTC-offset comment to discard.
//
// An adversarial review of the original #191 fix caught a regression here:
// that version parsed the 3-token form's date/time fields as UTC-labeled
// text and dropped the zone suffix, which shifted every intraday bar's
// instant 4-5h early (NY is UTC-4 in EDT / UTC-5 in EST). Downstream,
// intraday.sessionOpenAndVolume converts each bar back to NY wall-clock and
// filters on `Hour() < 9`, so the true 09:30 ET open bar re-appeared at
// 05:30 ET and was rejected outright — the reported "open" came from a much
// later bar, and session volume dropped every bar between 09:30 and
// whichever later bar first passed the filter.
//
// The fix: resolve the trailing token as an IANA zone name via
// time.LoadLocation and parse the wall-clock fields in *that* zone. IB's
// token (e.g. "US/Eastern") is a legacy but valid IANA name, so this
// round-trips correctly against the local tzdata. If the token doesn't
// resolve (unrecognized zone, missing tzdata entry), fall back to
// intelshared.NY(): every parseIBDate caller in this codebase deals in US
// stock bars, whose session zone is always US/Eastern, so NY is a safe
// default rather than a guess — never fall back to UTC, which is exactly
// the mislabeling bug this fix corrects.
//
// The 1-token (date-only) and 2-token (no-zone datetime) forms are
// unaffected and keep parsing as UTC-labeled text, matching prior behavior.
func parseIBDate(s string) (time.Time, error) {
	fields := strings.Fields(s)
	switch len(fields) {
	case 0:
		return time.Time{}, fmt.Errorf("parseIBDate: empty date string")
	case 1:
		return time.ParseInLocation("20060102", fields[0], time.UTC)
	case 2:
		return time.ParseInLocation("20060102 15:04:05", fields[0]+" "+fields[1], time.UTC)
	default:
		loc, err := time.LoadLocation(fields[2])
		if err != nil {
			loc = intelshared.NY()
		}
		return time.ParseInLocation("20060102 15:04:05", fields[0]+" "+fields[1], loc)
	}
}

// historicalDuration picks an IB ReqHistoricalData `durationStr` sized to the
// requested startDate..endDate span.
//
// startDate == "" preserves the original coarse default (a full year) used
// by callers that don't supply a range at all (e.g. daily-bar fetches
// elsewhere in the codebase) — unchanged so this fix doesn't ripple into
// unrelated call paths.
//
// When a startDate IS supplied (e.g. the intraday package derives one from
// its lookback window, see internal/intraday/adapter.go), the previous code
// blindly used "6 M" regardless of how short the actual span was. For a
// sub-day intraday lookback, IB rejects a 6-month request for 5-min bars
// (pacing violation) and silently returns nothing (#191 finding 1). Bucket
// the duration to the actual distance instead.
func historicalDuration(startDate, endDate string, now time.Time) string {
	if startDate == "" {
		return "1 Y"
	}
	start, err := parseIBDate(startDate)
	if err != nil {
		return "6 M"
	}
	end := now
	if endDate != "" {
		if e, err := parseIBDate(endDate); err == nil {
			end = e
		}
	}
	days := end.Sub(start).Hours() / 24
	switch {
	case days < 2:
		return "1 D"
	case days < 8:
		return "1 W"
	case days < 32:
		return "1 M"
	default:
		return "6 M"
	}
}

// GetPositions fetches all current account holdings via ReqPositions.
// ReqPositions is account-global with no reqID — errors arrive with reqID -1
// and cannot be routed to a pending request, so this relies on a 15s context
// timeout as the backstop (see spec Constraint 4).
func (c *Client) GetPositions(ctx context.Context) ([]model.Position, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("not connected to IB Gateway/TWS")
	}

	pp := c.wrapper.registerPositions()
	defer c.wrapper.unregisterPositions()

	c.ibClient.ReqPositions()
	defer c.ibClient.CancelPositions()

	posCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	select {
	case <-pp.done:
		return pp.positions, nil
	case <-posCtx.Done():
		return nil, fmt.Errorf("GetPositions: timeout fetching positions")
	}
}

// GetExecutions fetches the last-7-days execution history via ReqExecutions,
// narrowed by the supplied filter.
// NOTE: Side is NOT passed to IBKR's ExecutionFilter. IBKR expects "BUY"/"SELL"
// there, but the values in ExecDetails are "BOT"/"SLD". Passing an unrecognized
// Side causes IBKR to silently send neither ExecDetailsEnd nor an error, hanging
// the call forever. We filter by Side client-side instead.
func (c *Client) GetExecutions(ctx context.Context, filter model.ExecutionFilter) ([]model.Execution, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("not connected to IB Gateway/TWS")
	}

	reqID := c.nextReqID()
	pe := c.wrapper.registerExecutions(reqID)
	errCh := c.wrapper.registerError(reqID)
	defer c.wrapper.unregister(reqID)

	f := ibapi.NewExecutionFilter()
	if filter.Symbol != "" {
		f.Symbol = filter.Symbol
	}
	if !filter.Since.IsZero() {
		f.Time = filter.Since.Format("20060102-15:04:05")
	}

	c.ibClient.ReqExecutions(reqID, f)

	execCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	select {
	case <-pe.done:
		// fall through to side filtering below
	case err := <-errCh:
		return nil, fmt.Errorf("GetExecutions: %w", err)
	case <-execCtx.Done():
		return nil, fmt.Errorf("GetExecutions: timeout fetching executions")
	}

	execs := pe.executions
	if filter.Side != "" {
		want := strings.ToUpper(filter.Side)
		filtered := make([]model.Execution, 0, len(execs))
		for _, e := range execs {
			if strings.ToUpper(e.Side) == want {
				filtered = append(filtered, e)
			}
		}
		execs = filtered
	}
	return execs, nil
}

// GetOptionQuote returns a single option contract's mark price. It keeps the
// historical AccountService contract by collapsing the detailed quote into a
// single mark-like price.
func (c *Client) GetOptionQuote(ctx context.Context, underlying, expiration, right string, strike float64) (float64, error) {
	quote, err := c.GetOptionQuoteDetails(ctx, underlying, expiration, right, strike)
	if err != nil {
		return 0, err
	}
	if quote == nil || quote.Mark <= 0 {
		// GetOptionQuoteDetails never returns errCh failures as `err` — it
		// folds them into quote.Warnings as "ibkr_error: ..." instead (see
		// below). Surface that real reason here rather than collapsing every
		// failure (bad contract, no subscription, timeout, ...) into the
		// same generic "no price data" — position marking callers need to
		// tell "IB error 200: no security definition" apart from an ordinary
		// timeout (#193 finding 5).
		detail := "no price data"
		if ibErr := firstIBKRErrorWarning(quote); ibErr != "" {
			detail = ibErr
		}
		return 0, fmt.Errorf("GetOptionQuote %s %s %s %.2f: %s", underlying, expiration, right, strike, detail)
	}
	return quote.Mark, nil
}

// firstIBKRErrorWarning extracts the first "ibkr_error: ..." warning
// GetOptionQuoteDetails attached to a quote (see the ibkr_error prefix used
// in the warnings slice below), stripping the prefix so callers get the raw
// IB error text. Returns "" when the quote is nil or carries no such
// warning.
func firstIBKRErrorWarning(q *model.OptionQuote) string {
	if q == nil {
		return ""
	}
	const prefix = "ibkr_error: "
	for _, w := range q.Warnings {
		if strings.HasPrefix(w, prefix) {
			return strings.TrimPrefix(w, prefix)
		}
	}
	return ""
}

// GetOptionQuoteDetails returns a single option contract quote from IBKR with
// enough detail to validate scanner candidates. Missing subscription/data
// fields are surfaced through Warnings instead of silently treating zeroes as
// usable values.
func (c *Client) GetOptionQuoteDetails(ctx context.Context, underlying, expiration, right string, strike float64) (*model.OptionQuote, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("not connected to IB Gateway/TWS")
	}

	underlying = strings.ToUpper(strings.TrimSpace(underlying))
	expiration = strings.ReplaceAll(strings.TrimSpace(expiration), "-", "")
	right = normalizeOptionRight(right)
	if underlying == "" {
		return nil, fmt.Errorf("underlying is required")
	}
	if expiration == "" {
		return nil, fmt.Errorf("expiration is required")
	}
	if right == "" {
		return nil, fmt.Errorf("right must be C/call or P/put")
	}
	if strike <= 0 {
		return nil, fmt.Errorf("strike must be positive")
	}

	reqID := c.nextReqID()
	pq := c.wrapper.registerQuote(reqID, right)
	// Strict routing: this is option-quote's own contract-validation path,
	// so a whitelisted sub-2000 IB error (e.g. 200 "no security
	// definition") must fail fast instead of being dropped as
	// informational noise (#193 finding 1). 354 is excluded from the
	// strict set even here — see strictErrorCodes' doc comment — so a
	// "not subscribed" error doesn't kill a quote that delayed ticks would
	// still satisfy.
	errCh := c.wrapper.registerStrictError(reqID)
	defer c.wrapper.unregister(reqID)

	const genericTicks = "100,101,106,221"
	c.ibClient.ReqMktData(reqID, optionContract(underlying, expiration, right, strike), genericTicks, false, false, nil)
	defer c.ibClient.CancelMktData(reqID)

	timer := time.NewTimer(optionQuoteTimeout)
	defer timer.Stop()

	warnings := make([]string, 0, 2)
	select {
	case <-pq.done:
		select {
		case err := <-errCh:
			warnings = append(warnings, "ibkr_error: "+err.Error())
		default:
		}
	case <-timer.C:
	case err := <-errCh:
		warnings = append(warnings, "ibkr_error: "+err.Error())
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	quote := pq.snapshot()
	return optionQuoteFromSnapshot(underlying, expiration, right, strike, quote, warnings), nil
}

func normalizeOptionRight(right string) string {
	switch strings.ToUpper(strings.TrimSpace(right)) {
	case "C", "CALL":
		return "C"
	case "P", "PUT":
		return "P"
	default:
		return ""
	}
}

func optionTypeFromRight(right string) model.OptionType {
	if normalizeOptionRight(right) == "P" {
		return model.OptionTypePut
	}
	return model.OptionTypeCall
}

func optionQuoteFromSnapshot(underlying, expiration, right string, strike float64, snap quoteSnapshot, warnings []string) *model.OptionQuote {
	mid := 0.0
	if snap.bid > 0 && snap.ask > 0 {
		mid = (snap.bid + snap.ask) / 2
	}
	mark := quoteMark(snap.mark, snap.last, snap.bid, snap.ask, snap.close)
	q := &model.OptionQuote{
		Underlying:        underlying,
		Expiration:        expiration,
		Strike:            strike,
		OptionType:        optionTypeFromRight(right),
		Last:              snap.last,
		Bid:               snap.bid,
		Ask:               snap.ask,
		Mid:               mid,
		Mark:              mark,
		Volume:            int64(snap.volume),
		OpenInterest:      snap.openInterest,
		ImpliedVolatility: snap.impliedVolatility,
		Greeks:            snap.greeks,
		Timestamp:         time.Now().UTC(),
		MarketDataType:    snap.marketDataType,
		Warnings:          warnings,
	}
	return q
}
