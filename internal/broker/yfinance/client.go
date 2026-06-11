// Package yfinance implements broker.Broker using Yahoo Finance data via a Python subprocess.
// It serves as a fallback when IBKR TWS/Gateway is unavailable.
//
// Limitations vs IBKR:
//   - Data is delayed (not real-time)
//   - Bid/Ask may be stale or zero
//   - Greeks are not available (yfinance does not expose them); only IV is provided
package yfinance

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/pkg/model"
)

// fetcherSource is the Python script invoked as a subprocess to call into the
// yfinance library. It's embedded at compile time so the binary is fully
// self-contained — release tarballs don't ship the .py separately and don't
// need cwd-relative path resolution (which broke under -trimpath builds where
// runtime.Caller returns module-relative paths).
//
//go:embed fetcher.py
var fetcherSource []byte

// fetcherPath caches the path to a temp file holding fetcherSource.
// Materialized once per process; subsequent calls reuse it.
var (
	fetcherOnce sync.Once
	fetcherPath string
	fetcherErr  error
)

// resolveFetcher writes fetcherSource to a stable per-build temp file and
// returns its path. The filename embeds a SHA-256 of the script so different
// builds (with different fetcher.py contents) don't collide, and re-runs of
// the same build skip the write entirely.
func resolveFetcher() (string, error) {
	fetcherOnce.Do(func() {
		sum := sha256.Sum256(fetcherSource)
		name := fmt.Sprintf("optix-fetcher-%s.py", hex.EncodeToString(sum[:8]))
		dir := os.TempDir()
		path := filepath.Join(dir, name)

		// Idempotent: if the file already exists with the right contents, reuse.
		// (Common case: same binary invoked many times in the same /tmp.)
		if existing, err := os.ReadFile(path); err == nil && len(existing) == len(fetcherSource) {
			fetcherPath = path
			return
		}

		// Write atomically via a temp+rename so a concurrent invocation doesn't
		// see a half-written file.
		tmp, err := os.CreateTemp(dir, name+".*")
		if err != nil {
			fetcherErr = fmt.Errorf("yfinance: stage fetcher.py: %w", err)
			return
		}
		if _, err := tmp.Write(fetcherSource); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			fetcherErr = fmt.Errorf("yfinance: write fetcher.py: %w", err)
			return
		}
		if err := tmp.Close(); err != nil {
			os.Remove(tmp.Name())
			fetcherErr = fmt.Errorf("yfinance: close fetcher.py: %w", err)
			return
		}
		if err := os.Rename(tmp.Name(), path); err != nil {
			os.Remove(tmp.Name())
			fetcherErr = fmt.Errorf("yfinance: rename fetcher.py: %w", err)
			return
		}
		fetcherPath = path
	})
	return fetcherPath, fetcherErr
}

// Config holds yfinance client configuration.
type Config struct {
	// PythonBin is the path to the Python interpreter.
	// If empty, defaults to "python3".
	PythonBin string
}

// Client implements broker.Broker using Yahoo Finance data.
type Client struct {
	cfg Config
}

// Compile-time guarantees that yfinance.Client satisfies broker.Broker and
// broker.OIFetcher. The latter lets max-pain and other OI-aware callers use
// the yfinance fallback transparently.
var (
	_ broker.Broker    = (*Client)(nil)
	_ broker.OIFetcher = (*Client)(nil)
)

// New creates a new yfinance broker client.
func New(cfg Config) *Client {
	if cfg.PythonBin == "" {
		cfg.PythonBin = "python3"
	}
	return &Client{cfg: cfg}
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
		Symbol:        raw.Symbol,
		Last:          raw.Last,
		Bid:           raw.Bid,
		Ask:           raw.Ask,
		Volume:        raw.Volume,
		Change:        raw.Change,
		ChangePct:     raw.ChangePct,
		High:          raw.High,
		Low:           raw.Low,
		Open:          raw.Open,
		Close:         raw.Close,
		High52W:       raw.High52W,
		Low52W:        raw.Low52W,
		AvgVolume:     raw.AvgVolume,
		Timestamp:     ts,
		MarketSession: model.USMarketSession(time.Now()),
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
	return parseBarsJSON(out)
}

// parseBarsJSON converts yfinance's bars JSON response into a slice of
// model.OHLCV. Bars whose timestamp can't be parsed by any known layout are
// dropped (and logged) — letting them through with a zero Time would poison
// the SQLite dedup key (`Timestamp.UTC().Truncate(24h).Format(RFC3339)`),
// collapsing every bad bar onto the same "0001-01-01T00:00:00Z" row across
// symbols. See #31.
func parseBarsJSON(data []byte) ([]model.OHLCV, error) {
	var rawBars []struct {
		Timestamp string  `json:"timestamp"`
		Open      float64 `json:"open"`
		High      float64 `json:"high"`
		Low       float64 `json:"low"`
		Close     float64 `json:"close"`
		Volume    int64   `json:"volume"`
	}
	if err := json.Unmarshal(data, &rawBars); err != nil {
		return nil, fmt.Errorf("parse yfinance bars: %w", err)
	}
	bars := make([]model.OHLCV, 0, len(rawBars))
	for _, b := range rawBars {
		ts := parseBarTimestamp(b.Timestamp)
		if ts.IsZero() {
			log.Printf("yfinance: parseBarsJSON: skipping bar with unparseable timestamp %q", b.Timestamp)
			continue
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

// parseBarTimestamp tries the two timestamp layouts yfinance has been
// observed to emit. Returns time.Time{} on no match — the caller is
// responsible for treating that as a skip signal (not as a valid datum).
func parseBarTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02T15:04:05-07:00", s); err == nil {
		return t
	}
	return time.Time{}
}

// GetOptionChain fetches an option chain via yfinance. `expiration` is the
// YYYYMMDD form; an empty string requests the nearest-available expiry.
//
// When `expiration` is specified but not in yfinance's available list, returns
// *broker.ErrExpiryNotAvailable with the full list of expirations yfinance
// does offer — parity with the IBKR path.
//
// yfinance does not expose Greeks; Delta/Gamma/Theta/Vega/Rho on each
// OptionQuote remain zero. Callers needing Greeks should use IBKR or compute
// them via the Python analysis engine.
func (c *Client) GetOptionChain(ctx context.Context, underlying string, expiration string) (*model.OptionChain, error) {
	args := []string{"option_chain", underlying}
	if expiration != "" {
		args = append(args, expiration)
	}
	out, err := c.runFetcher(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("yfinance option_chain %s: %w", underlying, err)
	}

	var raw struct {
		Underlying  string `json:"underlying"`
		Expirations []struct {
			Expiration string         `json:"expiration"`
			Calls      []rawOptionRow `json:"calls"`
			Puts       []rawOptionRow `json:"puts"`
		} `json:"expirations"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse yfinance option_chain: %w", err)
	}

	chain := &model.OptionChain{Underlying: raw.Underlying}
	for _, e := range raw.Expirations {
		exp := model.OptionChainExpiry{Expiration: e.Expiration}
		for _, c := range e.Calls {
			exp.Calls = append(exp.Calls, c.toQuote(raw.Underlying, e.Expiration, model.OptionTypeCall))
		}
		for _, p := range e.Puts {
			exp.Puts = append(exp.Puts, p.toQuote(raw.Underlying, e.Expiration, model.OptionTypePut))
		}
		chain.Expirations = append(chain.Expirations, exp)
	}

	// When a specific expiry was requested, surface ErrExpiryNotAvailable if
	// the chain doesn't contain a populated entry for it. (Python emits the
	// available list as empty-contract entries on miss; the requested entry
	// is absent in that payload, so any populated entry signals a hit.)
	if expiration != "" {
		var matched *model.OptionChainExpiry
		for i := range chain.Expirations {
			e := &chain.Expirations[i]
			if e.Expiration == expiration && (len(e.Calls) > 0 || len(e.Puts) > 0) {
				matched = e
				break
			}
		}
		if matched == nil {
			avail := make([]string, 0, len(chain.Expirations))
			for _, e := range chain.Expirations {
				avail = append(avail, e.Expiration)
			}
			return nil, &broker.ErrExpiryNotAvailable{
				Underlying: underlying,
				Requested:  expiration,
				Available:  avail,
			}
		}
		// Narrow to just the matched expiry (parity with IBKR contract).
		chain.Expirations = []model.OptionChainExpiry{*matched}
	}
	return chain, nil
}

// GetOptionChainWithOI is the same as GetOptionChain for yfinance — yfinance
// returns openInterest inline on every contract, so there's no separate
// "with OI" cost or interface. Implementing this also lets yfinance.Client
// satisfy broker.OIFetcher so callers (e.g. max-pain) can use it transparently.
func (c *Client) GetOptionChainWithOI(ctx context.Context, underlying string, expiration string) (*model.OptionChain, error) {
	return c.GetOptionChain(ctx, underlying, expiration)
}

// rawOptionRow mirrors the JSON shape emitted by fetcher.py's option_chain
// subcommand — one row per contract.
type rawOptionRow struct {
	Strike       float64 `json:"strike"`
	Bid          float64 `json:"bid"`
	Ask          float64 `json:"ask"`
	Last         float64 `json:"last"`
	Volume       int64   `json:"volume"`
	OpenInterest int32   `json:"openInterest"`
	IV           float64 `json:"iv"`
}

func (r rawOptionRow) toQuote(underlying, expiration string, kind model.OptionType) model.OptionQuote {
	mid := 0.0
	if r.Bid > 0 && r.Ask > 0 {
		mid = (r.Bid + r.Ask) / 2
	}
	return model.OptionQuote{
		Underlying:        underlying,
		Expiration:        expiration,
		Strike:            r.Strike,
		OptionType:        kind,
		Last:              r.Last,
		Bid:               r.Bid,
		Ask:               r.Ask,
		Mid:               mid,
		Volume:            r.Volume,
		OpenInterest:      r.OpenInterest,
		ImpliedVolatility: r.IV,
	}
}

// RunFetcher executes the embedded fetcher.py with the given args via
// pythonBin, returning stdout. Exported for internal/marketdata, which reuses
// the same staged script for batch market-data fetches. Dependency direction
// is one-way (marketdata → yfinance); broker code never imports marketdata.
func RunFetcher(ctx context.Context, pythonBin string, args ...string) ([]byte, error) {
	if pythonBin == "" {
		pythonBin = "python3"
	}
	fetcher, err := resolveFetcher()
	if err != nil {
		return nil, err
	}
	cmdArgs := append([]string{fetcher}, args...)
	cmd := exec.CommandContext(ctx, pythonBin, cmdArgs...)
	// WaitDelay closes the stdout pipe 5s after ctx expires, so an orphaned
	// grandchild (e.g. fetcher.py's _ensure_yfinance pip install) cannot hold
	// the write-end open and cause cmd.Output() to block indefinitely.
	cmd.WaitDelay = 5 * time.Second

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s: %s", err, string(exitErr.Stderr))
		}
		return nil, err
	}
	return out, nil
}

func (c *Client) runFetcher(ctx context.Context, args ...string) ([]byte, error) {
	return RunFetcher(ctx, c.cfg.PythonBin, args...)
}
