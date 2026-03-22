package broker

import (
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/broker/yfinance"
)

// NewWithFallback creates a FallbackBroker that tries IBKR first,
// then falls back to Yahoo Finance if IBKR is unavailable.
//
// pythonBin is the path to the Python interpreter for yfinance.
// If empty, defaults to "python3".
func NewWithFallback(ibCfg ibkr.Config, pythonBin string) *FallbackBroker {
	primary := ibkr.New(ibCfg)
	fallback := yfinance.New(yfinance.Config{PythonBin: pythonBin})
	return NewFallbackBroker(primary, fallback)
}
