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
	primary     Broker
	fallback    Broker
	active      Broker // whichever connected successfully
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
func (fb *FallbackBroker) Connect(ctx context.Context) error {
	err := fb.primary.Connect(ctx)
	if err == nil {
		fb.active = fb.primary
		fb.usingFallback = false
		return nil
	}

	log.Printf("⚠️  IBKR unavailable (%v), falling back to Yahoo Finance (delayed data, no options)", err)

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

// GetQuote delegates to the active broker.
func (fb *FallbackBroker) GetQuote(ctx context.Context, symbol string) (*model.StockQuote, error) {
	return fb.active.GetQuote(ctx, symbol)
}

// GetHistoricalBars delegates to the active broker.
func (fb *FallbackBroker) GetHistoricalBars(ctx context.Context, symbol, timeframe, startDate, endDate string) ([]model.OHLCV, error) {
	return fb.active.GetHistoricalBars(ctx, symbol, timeframe, startDate, endDate)
}

// GetOptionChain delegates to the active broker.
// When using fallback, this returns an empty chain (options require IBKR).
func (fb *FallbackBroker) GetOptionChain(ctx context.Context, underlying string, expiration string) (*model.OptionChain, error) {
	return fb.active.GetOptionChain(ctx, underlying, expiration)
}
