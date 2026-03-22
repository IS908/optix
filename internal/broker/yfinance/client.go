// Package yfinance implements broker.Broker using Yahoo Finance data via a Python subprocess.
// It serves as a fallback when IBKR TWS/Gateway is unavailable.
//
// Limitations vs IBKR:
//   - Data is delayed (not real-time)
//   - GetOptionChain always returns an empty chain (options require IBKR)
//   - Bid/Ask may be stale or zero
package yfinance

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// Config holds yfinance client configuration.
type Config struct {
	// PythonBin is the path to the Python interpreter.
	// If empty, defaults to "python3".
	PythonBin string
}

// Client implements broker.Broker using Yahoo Finance data.
type Client struct {
	cfg       Config
	fetcherPy string // absolute path to fetcher.py
}

// New creates a new yfinance broker client.
func New(cfg Config) *Client {
	if cfg.PythonBin == "" {
		cfg.PythonBin = "python3"
	}

	// Resolve fetcher.py relative to this source file
	_, thisFile, _, _ := runtime.Caller(0)
	fetcherPy := filepath.Join(filepath.Dir(thisFile), "fetcher.py")

	return &Client{
		cfg:       cfg,
		fetcherPy: fetcherPy,
	}
}

// Connect is a no-op for yfinance (no persistent connection needed).
func (c *Client) Connect(ctx context.Context) error {
	return nil
}

// Disconnect is a no-op for yfinance.
func (c *Client) Disconnect() error {
	return nil
}

// IsConnected always returns true for yfinance (stateless HTTP calls).
func (c *Client) IsConnected() bool {
	return true
}

// GetQuote retrieves the latest quote for a symbol via yfinance.
func (c *Client) GetQuote(ctx context.Context, symbol string) (*model.StockQuote, error) {
	out, err := c.runFetcher(ctx, "quote", symbol)
	if err != nil {
		return nil, fmt.Errorf("yfinance quote %s: %w", symbol, err)
	}

	var raw struct {
		Symbol    string  `json:"symbol"`
		Last      float64 `json:"last"`
		Bid       float64 `json:"bid"`
		Ask       float64 `json:"ask"`
		Volume    int64   `json:"volume"`
		Change    float64 `json:"change"`
		ChangePct float64 `json:"changePct"`
		High      float64 `json:"high"`
		Low       float64 `json:"low"`
		Open      float64 `json:"open"`
		Close     float64 `json:"close"`
		High52W   float64 `json:"high52w"`
		Low52W    float64 `json:"low52w"`
		AvgVolume float64 `json:"avgVolume"`
		Timestamp string  `json:"timestamp"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse yfinance quote: %w", err)
	}

	ts, _ := time.Parse(time.RFC3339Nano, raw.Timestamp)
	if ts.IsZero() {
		ts, _ = time.Parse("2006-01-02T15:04:05.999999", raw.Timestamp)
	}
	if ts.IsZero() {
		ts = time.Now()
	}

	return &model.StockQuote{
		Symbol:    raw.Symbol,
		Last:      raw.Last,
		Bid:       raw.Bid,
		Ask:       raw.Ask,
		Volume:    raw.Volume,
		Change:    raw.Change,
		ChangePct: raw.ChangePct,
		High:      raw.High,
		Low:       raw.Low,
		Open:      raw.Open,
		Close:     raw.Close,
		High52W:   raw.High52W,
		Low52W:    raw.Low52W,
		AvgVolume: raw.AvgVolume,
		Timestamp: ts,
	}, nil
}

// GetHistoricalBars retrieves historical OHLCV data via yfinance.
func (c *Client) GetHistoricalBars(ctx context.Context, symbol, timeframe, startDate, endDate string) ([]model.OHLCV, error) {
	// Calculate days from date range
	days := 365
	if startDate != "" && endDate != "" {
		start, err1 := time.Parse("20060102", startDate)
		end, err2 := time.Parse("20060102", endDate)
		if err1 == nil && err2 == nil {
			days = int(end.Sub(start).Hours() / 24)
			if days < 1 {
				days = 30
			}
		}
	}

	out, err := c.runFetcher(ctx, "bars", symbol, timeframe, fmt.Sprintf("%d", days))
	if err != nil {
		return nil, fmt.Errorf("yfinance bars %s: %w", symbol, err)
	}

	var rawBars []struct {
		Timestamp string  `json:"timestamp"`
		Open      float64 `json:"open"`
		High      float64 `json:"high"`
		Low       float64 `json:"low"`
		Close     float64 `json:"close"`
		Volume    int64   `json:"volume"`
	}
	if err := json.Unmarshal(out, &rawBars); err != nil {
		return nil, fmt.Errorf("parse yfinance bars: %w", err)
	}

	bars := make([]model.OHLCV, 0, len(rawBars))
	for _, b := range rawBars {
		ts, _ := time.Parse(time.RFC3339, b.Timestamp)
		if ts.IsZero() {
			ts, _ = time.Parse("2006-01-02T15:04:05-07:00", b.Timestamp)
		}
		bars = append(bars, model.OHLCV{
			Timestamp: ts,
			Open:      b.Open,
			High:      b.High,
			Low:       b.Low,
			Close:     b.Close,
			Volume:    b.Volume,
		})
	}
	return bars, nil
}

// GetOptionChain returns an empty option chain in yfinance fallback mode.
// Options data requires IBKR for real-time Greeks and accurate pricing.
func (c *Client) GetOptionChain(ctx context.Context, underlying string, expiration string) (*model.OptionChain, error) {
	return &model.OptionChain{
		Underlying:  underlying,
		Expirations: nil,
	}, nil
}

// runFetcher executes the Python fetcher script and returns its stdout.
func (c *Client) runFetcher(ctx context.Context, args ...string) ([]byte, error) {
	cmdArgs := append([]string{c.fetcherPy}, args...)
	cmd := exec.CommandContext(ctx, c.cfg.PythonBin, cmdArgs...)

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s: %s", err, string(exitErr.Stderr))
		}
		return nil, err
	}
	return out, nil
}
