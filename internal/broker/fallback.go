package broker

import (
	"context"
	"fmt"
	"log"

	"github.com/IS908/optix/pkg/model"
)

// FallbackBroker wraps a primary and fallback Broker implementation.
// On Connect(), it tries the primary; if that fails, all subsequent calls
// in this instance's lifetime go through the fallback.
//
// This gives per-request fallback semantics: each caller creates a new
// FallbackBroker, and the decision is made once at Connect() time.
type FallbackBroker struct {
	primary       Broker
	fallback      Broker
	active        Broker // whichever connected successfully
	usingFallback bool
}

// NewFallbackBroker creates a broker that tries primary first, then fallback.
func NewFallbackBroker(primary, fallback Broker) *FallbackBroker {
	return &FallbackBroker{
		primary:  primary,
		fallback: fallback,
	}
}

// Connect tries the primary broker; on failure, switches to fallback for this session.
// Explicitly disconnects the primary after failure to release any partial TCP connection
// that may have been established before the handshake timed out.
func (fb *FallbackBroker) Connect(ctx context.Context) error {
	err := fb.primary.Connect(ctx)
	if err == nil {
		fb.active = fb.primary
		fb.usingFallback = false
		return nil
	}

	// Ensure any partial IBKR TCP connection is released before falling back.
	// Connect() already calls Disconnect() internally on timeout/cancel, but we
	// call it again here defensively in case the ibapi library left state behind.
	_ = fb.primary.Disconnect()

	// If the ctx was cancelled (user Ctrl-C, parent timeout fired), respect
	// the cancellation rather than silently degrading to yfinance. yfinance's
	// Connect is a no-op and would happily "succeed" on a dead ctx — pre-fix
	// the user's abort was reinterpreted as "please use delayed data." See #41.
	if ctx.Err() != nil {
		return ctx.Err()
	}

	log.Printf("⚠️  IBKR unavailable (%v), falling back to Yahoo Finance (delayed data)", err)

	if fbErr := fb.fallback.Connect(ctx); fbErr != nil {
		return fmt.Errorf("both IBKR and Yahoo Finance unavailable: ibkr=%w, yfinance=%v", err, fbErr)
	}

	fb.active = fb.fallback
	fb.usingFallback = true
	return nil
}

// Disconnect closes the active broker connection.
func (fb *FallbackBroker) Disconnect() error {
	if fb.active != nil {
		return fb.active.Disconnect()
	}
	return nil
}

// IsConnected returns true if the active broker is connected.
func (fb *FallbackBroker) IsConnected() bool {
	if fb.active != nil {
		return fb.active.IsConnected()
	}
	return false
}

// UsingFallback returns true if this broker is using the yfinance fallback.
func (fb *FallbackBroker) UsingFallback() bool {
	return fb.usingFallback
}

// SourceName returns a display-friendly data source identifier.
func (fb *FallbackBroker) SourceName() string {
	if fb.usingFallback {
		return "Yahoo Finance"
	}
	return "IBKR"
}

// SourceBanner returns a one-line status banner showing data source and delay info.
// Suitable for printing after Connect().
func (fb *FallbackBroker) SourceBanner() string {
	if fb.usingFallback {
		return "📡 Data source: Yahoo Finance (delayed ~15 min)"
	}
	return "📡 Data source: IBKR (real-time)"
}

// Ping delegates to the active broker if it implements Pinger, otherwise
// returns nil (yfinance is stateless and always "connected").
func (fb *FallbackBroker) Ping(ctx context.Context) error {
	if fb.active == nil {
		return fmt.Errorf("broker: not connected")
	}
	if p, ok := fb.active.(Pinger); ok {
		return p.Ping(ctx)
	}
	return nil // fallback (yfinance) is always available
}

// GetQuote delegates to the active broker.
func (fb *FallbackBroker) GetQuote(ctx context.Context, symbol string) (*model.StockQuote, error) {
	return fb.active.GetQuote(ctx, symbol)
}

// GetHistoricalBars delegates to the active broker.
func (fb *FallbackBroker) GetHistoricalBars(ctx context.Context, symbol, timeframe, startDate, endDate string) ([]model.OHLCV, error) {
	return fb.active.GetHistoricalBars(ctx, symbol, timeframe, startDate, endDate)
}

// GetOptionChain delegates to the active broker. The yfinance fallback can
// return delayed option-chain data when IBKR is unavailable.
func (fb *FallbackBroker) GetOptionChain(ctx context.Context, underlying string, expiration string) (*model.OptionChain, error) {
	return fb.active.GetOptionChain(ctx, underlying, expiration)
}

// GetOptionChainWithOI delegates to the active broker if it implements OIFetcher.
// Returns ErrOINotSupported if the active broker is the yfinance fallback or
// any other broker that cannot fetch per-contract Open Interest.
func (fb *FallbackBroker) GetOptionChainWithOI(ctx context.Context, underlying string, expiration string) (*model.OptionChain, error) {
	if fb.active == nil {
		return nil, fmt.Errorf("broker: not connected")
	}
	if f, ok := fb.active.(OIFetcher); ok {
		return f.GetOptionChainWithOI(ctx, underlying, expiration)
	}
	return nil, ErrOINotSupported
}

// GetMarketDepth delegates to the active broker if it implements
// MarketDepthFetcher. Returns ErrMarketDepthNotSupported when running on a
// fallback or broker that cannot supply order-book depth.
func (fb *FallbackBroker) GetMarketDepth(ctx context.Context, symbol string, levels int) (*model.MarketDepth, error) {
	if fb.active == nil {
		return nil, fmt.Errorf("broker: not connected")
	}
	if f, ok := fb.active.(MarketDepthFetcher); ok {
		return f.GetMarketDepth(ctx, symbol, levels)
	}
	return nil, ErrMarketDepthNotSupported
}

// GetPositions delegates to the active broker if it implements AccountReader.
// Returns ErrAccountNotSupported when the active broker is the yfinance
// fallback (which has no account concept).
func (fb *FallbackBroker) GetPositions(ctx context.Context) ([]model.Position, error) {
	if fb.active == nil {
		return nil, fmt.Errorf("broker: not connected")
	}
	if ar, ok := fb.active.(AccountReader); ok {
		return ar.GetPositions(ctx)
	}
	return nil, ErrAccountNotSupported
}

// GetExecutions delegates to the active broker if it implements AccountReader.
// Returns ErrAccountNotSupported when running on the yfinance fallback.
func (fb *FallbackBroker) GetExecutions(ctx context.Context, filter model.ExecutionFilter) ([]model.Execution, error) {
	if fb.active == nil {
		return nil, fmt.Errorf("broker: not connected")
	}
	if ar, ok := fb.active.(AccountReader); ok {
		return ar.GetExecutions(ctx, filter)
	}
	return nil, ErrAccountNotSupported
}

// GetOptionQuote delegates to the active broker if it implements OptionQuoter.
// Returns ErrAccountNotSupported when running on the yfinance fallback.
func (fb *FallbackBroker) GetOptionQuote(ctx context.Context, underlying, expiration, right string, strike float64) (float64, error) {
	if fb.active == nil {
		return 0, fmt.Errorf("broker: not connected")
	}
	if oq, ok := fb.active.(OptionQuoter); ok {
		return oq.GetOptionQuote(ctx, underlying, expiration, right, strike)
	}
	return 0, ErrAccountNotSupported
}
