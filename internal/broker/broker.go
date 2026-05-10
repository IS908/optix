package broker

import (
	"context"
	"errors"

	"github.com/IS908/optix/pkg/model"
)

// Pinger is an optional interface for brokers that support lightweight
// connectivity probes. If a broker implements Pinger, the connection pool
// uses it for active health checks instead of relying solely on IsConnected().
type Pinger interface {
	Ping(ctx context.Context) error
}

// OIFetcher is an optional interface for brokers that can populate
// per-contract Open Interest on top of the structure-only chain returned
// by GetOptionChain. Currently only IBKR implements this — yfinance returns
// ErrOINotSupported.
type OIFetcher interface {
	// GetOptionChainWithOI returns the option chain enriched with OpenInterest
	// values. Implementations may limit to the nearest expiration and a strike
	// window around the current price for performance.
	GetOptionChainWithOI(ctx context.Context, underlying string, expiration string) (*model.OptionChain, error)
}

// ErrOINotSupported is returned by brokers that cannot provide per-contract
// Open Interest (e.g., yfinance fallback).
var ErrOINotSupported = errors.New("broker does not support per-contract Open Interest (requires IBKR)")

// Broker defines the interface for interacting with a brokerage.
type Broker interface {
	// Connect establishes a connection to the broker.
	Connect(ctx context.Context) error

	// Disconnect closes the connection.
	Disconnect() error

	// IsConnected returns true if connected.
	IsConnected() bool

	// GetQuote retrieves the latest quote for a symbol.
	GetQuote(ctx context.Context, symbol string) (*model.StockQuote, error)

	// GetHistoricalBars retrieves historical OHLCV data.
	GetHistoricalBars(ctx context.Context, symbol, timeframe, startDate, endDate string) ([]model.OHLCV, error)

	// GetOptionChain retrieves the option chain for an underlying.
	GetOptionChain(ctx context.Context, underlying string, expiration string) (*model.OptionChain, error)
}
