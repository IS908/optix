package ibkr

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/pkg/model"
	"github.com/scmhub/ibapi"
)

// Config holds IB TWS connection settings.
type Config struct {
	Host     string
	Port     int
	ClientID int64
}

// Client wraps the IB TWS API connection.
type Client struct {
	cfg      Config
	wrapper  *IbWrapper
	ibClient *ibapi.EClient

	mu          sync.RWMutex
	connected   bool
	watchCancel context.CancelFunc // stops the disconnect-watcher goroutine; nil if not running

	reqIDCounter int64 // atomic counter for request IDs
}

// New creates a new IB TWS client.
func New(cfg Config) *Client {
	wrapper := newIbWrapper()
	ibClient := ibapi.NewEClient(wrapper)
	return &Client{
		cfg:      cfg,
		wrapper:  wrapper,
		ibClient: ibClient,
	}
}

// Connect establishes a connection to IB TWS or Gateway.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	err := c.ibClient.Connect(c.cfg.Host, c.cfg.Port, c.cfg.ClientID)
	if err != nil {
		return fmt.Errorf("connect to IB TWS at %s:%d: %w", c.cfg.Host, c.cfg.Port, err)
	}

	// Wait for NextValidID (signals handshake complete) with a timeout.
	select {
	case firstID := <-c.wrapper.nextValidID:
		atomic.StoreInt64(&c.reqIDCounter, firstID)
	case <-time.After(10 * time.Second):
		if dErr := c.ibClient.Disconnect(); dErr != nil {
			log.Printf("ibkr: disconnect after handshake timeout (clientID %d): %v", c.cfg.ClientID, dErr)
		}
		return fmt.Errorf("timeout waiting for IB TWS handshake")
	case <-ctx.Done():
		if dErr := c.ibClient.Disconnect(); dErr != nil {
			log.Printf("ibkr: disconnect after context cancel (clientID %d): %v", c.cfg.ClientID, dErr)
		}
		return ctx.Err()
	}

	// Use delayed market data (type 3) so the client works without
	// a real-time API subscription.  Accounts that do have live
	// subscriptions will still receive live data for those symbols;
	// for everything else IB falls back to the 15-min delayed feed.
	c.ibClient.ReqMarketDataType(3)

	c.connected = true

	// Drain any stale disconnect signal that may linger from a prior session.
	select {
	case <-c.wrapper.disconnectCh:
	default:
	}
	// Start a goroutine that watches for TCP-drop callbacks from ibapi and
	// immediately marks the client as disconnected without waiting for the
	// next health check.
	watchCtx, watchCancel := context.WithCancel(context.Background())
	if c.watchCancel != nil {
		c.watchCancel() // stop previous watcher if any
	}
	c.watchCancel = watchCancel
	go c.watchDisconnect(watchCtx)

	return nil
}

// Disconnect closes the IB TWS connection.
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}
	err := c.ibClient.Disconnect()
	c.connected = false
	// Stop the watcher goroutine so it doesn't fire after an intentional disconnect.
	if c.watchCancel != nil {
		c.watchCancel()
		c.watchCancel = nil
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
		c.connected = false
		c.mu.Unlock()
		log.Printf("ibkr: TCP connection dropped (clientID %d) — slot marked unhealthy", c.cfg.ClientID)
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
		return nil, fmt.Errorf("not connected to IB TWS")
	}

	session := model.USMarketSession(time.Now())

	reqID := c.nextReqID()
	pq := c.wrapper.registerQuote(reqID)
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
	last := quote.last
	// During extended hours, prefer bid/ask midpoint when no LAST trade has
	// occurred yet in the current session — this gives a more meaningful price
	// than falling back to previous close.
	if session.IsExtendedHours() && last == 0 && quote.bid > 0 && quote.ask > 0 {
		last = (quote.bid + quote.ask) / 2
	}
	if last == 0 {
		last = (quote.bid + quote.ask) / 2 // midpoint fallback when market is closed
	}
	if last == 0 && quote.close > 0 {
		last = quote.close // previous close
	}

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
		Close:         quote.close,
		Volume:        int64(quote.volume),
		Timestamp:     time.Now(),
		MarketSession: session,
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
		return nil, fmt.Errorf("not connected to IB TWS")
	}

	reqID := c.nextReqID()
	pb := c.wrapper.registerBars(reqID)
	errCh := c.wrapper.registerError(reqID)
	defer c.wrapper.unregister(reqID)

	// duration covers roughly the requested period.  Keep it simple: 1 Y.
	duration := "1 Y"
	if startDate != "" {
		duration = "6 M"
	}

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
		return nil, fmt.Errorf("not connected to IB TWS")
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
	if qErr != nil || quote == nil || quote.Last <= 0 {
		// Fall back to using the median strike as a proxy.
		log.Printf("ibkr: GetOptionChainWithOI %s: spot quote unavailable (%v) — using median strike", underlying, qErr)
		if len(chain.Expirations) > 0 && len(chain.Expirations[0].Calls) > 0 {
			strikes := chain.Expirations[0].Calls
			median := strikes[len(strikes)/2].Strike
			quote = &model.StockQuote{Last: median}
		} else {
			return chain, nil
		}
	}

	spot := quote.Last
	if spot > 0 {
		chain.UnderlyingPrice = spot
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
	last := bars[len(bars)-1]
	// Reconstruct a best-effort quote from the last daily bar.
	return &model.StockQuote{
		Symbol:    symbol,
		Last:      last.Close,
		Close:     last.Close,
		Open:      last.Open,
		High:      last.High,
		Low:       last.Low,
		Volume:    last.Volume,
		Timestamp: last.Timestamp,
	}, nil
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

// parseIBDate parses IB historical bar date strings ("20240101" or "20240101 09:30:00").
// Parsed in UTC to ensure consistent storage regardless of server timezone.
func parseIBDate(s string) (time.Time, error) {
	if len(s) == 8 {
		return time.ParseInLocation("20060102", s, time.UTC)
	}
	return time.ParseInLocation("20060102 15:04:05", s, time.UTC)
}

// GetPositions fetches all current account holdings via ReqPositions.
// ReqPositions is account-global with no reqID — errors arrive with reqID -1
// and cannot be routed to a pending request, so this relies on a 15s context
// timeout as the backstop (see spec Constraint 4).
func (c *Client) GetPositions(ctx context.Context) ([]model.Position, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("not connected to IB TWS")
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
		return nil, fmt.Errorf("not connected to IB TWS")
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

// GetOptionQuote returns a single option contract's mark price. It mirrors
// GetQuote but builds an OPT contract; the mark is LAST, falling back to the
// bid/ask midpoint, then CLOSE. Returns an error (e.g. IB 10091 — no OPRA
// subscription) when no price is available.
func (c *Client) GetOptionQuote(ctx context.Context, underlying, expiration, right string, strike float64) (float64, error) {
	if !c.IsConnected() {
		return 0, fmt.Errorf("not connected to IB TWS")
	}

	reqID := c.nextReqID()
	pq := c.wrapper.registerQuote(reqID)
	errCh := c.wrapper.registerError(reqID)
	defer c.wrapper.unregister(reqID)

	c.ibClient.ReqMktData(reqID, optionContract(underlying, expiration, right, strike), "", false, false, nil)
	defer c.ibClient.CancelMktData(reqID)

	tickCtx, tickCancel := context.WithTimeout(ctx, 5*time.Second)
	defer tickCancel()

	select {
	case <-pq.done:
	case <-tickCtx.Done():
	case err := <-errCh:
		return 0, fmt.Errorf("GetOptionQuote %s %s %s %.2f: %w", underlying, expiration, right, strike, err)
	}

	quote := pq.snapshot()
	mark := quote.last
	if mark == 0 && quote.bid > 0 && quote.ask > 0 {
		mark = (quote.bid + quote.ask) / 2
	}
	if mark == 0 {
		mark = quote.close
	}
	if mark == 0 {
		return 0, fmt.Errorf("GetOptionQuote %s %s %s %.2f: no price data", underlying, expiration, right, strike)
	}
	return mark, nil
}
