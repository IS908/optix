package ibkr

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

	mu        sync.RWMutex
	connected bool

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
		_ = c.ibClient.Disconnect()
		return fmt.Errorf("timeout waiting for IB TWS handshake")
	case <-ctx.Done():
		_ = c.ibClient.Disconnect()
		return ctx.Err()
	}

	// Use delayed market data (type 3) so the client works without
	// a real-time API subscription.  Accounts that do have live
	// subscriptions will still receive live data for those symbols;
	// for everything else IB falls back to the 15-min delayed feed.
	c.ibClient.ReqMarketDataType(3)

	c.connected = true
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
	return err
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

// GetQuote retrieves the latest stock quote from IB.
//
// Uses streaming market data with a short collection window so it works with
// both snapshot-capable (live) and non-snapshot (delayed) data types.
func (c *Client) GetQuote(ctx context.Context, symbol string) (*model.StockQuote, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("not connected to IB TWS")
	}

	reqID := c.nextReqID()
	pq := c.wrapper.registerQuote(reqID)
	errCh := c.wrapper.registerError(reqID)
	defer c.wrapper.unregister(reqID)

	// snapshot=false → streaming; we cancel after TickSnapshotEnd fires OR
	// after a short timeout, whichever comes first.  This works for both live
	// and delayed (type 3) data where snapshot permissions may be 0.
	c.ibClient.ReqMktData(reqID, stockContract(symbol), "", false, false, nil)

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
			return c.quoteFromHistory(ctx, symbol)
		}
		return nil, fmt.Errorf("GetQuote %s: %w", symbol, err)
	}

	// Always cancel the stream so TWS stops sending updates.
	c.ibClient.CancelMktData(reqID)

	last := pq.last
	if last == 0 {
		last = (pq.bid + pq.ask) / 2 // midpoint fallback when market is closed
	}
	if last == 0 && pq.close > 0 {
		last = pq.close // previous close
	}

	// If streaming yielded no price data at all, fall back to historical close.
	if last == 0 {
		return c.quoteFromHistory(ctx, symbol)
	}

	return &model.StockQuote{
		Symbol:    symbol,
		Last:      last,
		Bid:       pq.bid,
		Ask:       pq.ask,
		Close:     pq.close,
		Volume:    int64(pq.volume),
		Timestamp: time.Now(),
	}, nil
}

// GetHistoricalBars retrieves historical OHLCV data from IB.
//
// timeframe examples: "1 day", "1 hour", "5 mins"
// startDate / endDate: "20240101 00:00:00 US/Eastern" or ""
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

	c.ibClient.ReqHistoricalData(
		reqID, stockContract(symbol),
		endDate,   // endDateTime ("" = now)
		duration,  // durationStr
		timeframe, // barSizeSetting e.g. "1 day"
		"TRADES",  // whatToShow
		true,      // useRTH
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
	if expiration != "" {
		// Caller wants a specific expiry.
		expirations = []string{expiration}
	} else {
		// Default: return the 4 nearest expirations (enough for 2-week analysis).
		if len(expirations) > 4 {
			expirations = expirations[:4]
		}
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

	return chain, nil
}

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

// parseIBDate parses IB historical bar date strings ("20240101" or "20240101 09:30:00").
// Parsed in UTC to ensure consistent storage regardless of server timezone.
func parseIBDate(s string) (time.Time, error) {
	if len(s) == 8 {
		return time.ParseInLocation("20060102", s, time.UTC)
	}
	return time.ParseInLocation("20060102 15:04:05", s, time.UTC)
}

