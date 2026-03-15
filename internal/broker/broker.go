package broker

import (
	"context"

	"github.com/IS908/optix/pkg/model"
)

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
