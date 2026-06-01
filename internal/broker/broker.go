package broker

import (
	"context"
	"errors"
	"fmt"

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
// by GetOptionChain. Both IBKR and yfinance implement this; brokers that
// cannot supply per-contract OI return ErrOINotSupported.
type OIFetcher interface {
	// GetOptionChainWithOI returns the option chain enriched with OpenInterest
	// values. Implementations may limit to the nearest expiration and a strike
	// window around the current price for performance.
	GetOptionChainWithOI(ctx context.Context, underlying string, expiration string) (*model.OptionChain, error)
}

// ErrOINotSupported is returned by brokers that cannot provide per-contract
// Open Interest.
var ErrOINotSupported = errors.New("broker does not support per-contract Open Interest")

// ErrExpiryNotAvailable is returned by GetOptionChainWithOI when the caller
// requested a specific expiration that the broker does not offer for this
// underlying. Available contains the full unsorted list of expirations the
// broker returned (caller is responsible for sorting/distance ranking).
type ErrExpiryNotAvailable struct {
	Underlying string   // e.g. "GOOGL"
	Requested  string   // YYYYMMDD as supplied to GetOptionChainWithOI
	Available  []string // YYYYMMDD, unsorted
}

func (e *ErrExpiryNotAvailable) Error() string {
	return fmt.Sprintf("expiry %s not available for %s (%d alternatives)",
		e.Requested, e.Underlying, len(e.Available))
}

// AccountReader is an optional interface for brokers that can read account
// holdings and execution history. IBKR implements it; yfinance does not.
type AccountReader interface {
	GetPositions(ctx context.Context) ([]model.Position, error)
	GetExecutions(ctx context.Context, filter model.ExecutionFilter) ([]model.Execution, error)
}

// OptionQuoter is an optional interface for brokers that can return a single
// option contract's mark price. Used by AccountService to mark option positions.
type OptionQuoter interface {
	GetOptionQuote(ctx context.Context, underlying, expiration, right string, strike float64) (float64, error)
}

// ErrAccountNotSupported is returned by brokers that cannot provide account
// data (e.g., the yfinance fallback).
var ErrAccountNotSupported = errors.New("broker does not support account data (requires IBKR)")

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
