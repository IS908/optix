// Package factory constructs concrete Broker implementations.
//
// It lives in its own subpackage (rather than in package broker) so that
// concrete brokers (ibkr, yfinance) can depend on `broker` for shared types
// like ErrExpiryNotAvailable without creating an import cycle.
package factory

import (
	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/broker/yfinance"
)

// NewWithFallback creates a FallbackBroker that tries IBKR first,
// then falls back to Yahoo Finance if IBKR is unavailable.
//
// pythonBin is the path to the Python interpreter for yfinance.
// If empty, defaults to "python3".
func NewWithFallback(ibCfg ibkr.Config, pythonBin string) *broker.FallbackBroker {
	primary := ibkr.New(ibCfg)
	fallback := yfinance.New(yfinance.Config{PythonBin: pythonBin})
	return broker.NewFallbackBroker(primary, fallback)
}
