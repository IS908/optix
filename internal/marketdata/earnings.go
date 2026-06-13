package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/IS908/optix/internal/broker/yfinance"
)

// EarningsEvent is a free/degraded earnings row from Yahoo Finance's
// earnings_dates view. Numeric fields are pointers because yfinance often has
// the timestamp before reported/estimate values are available.
type EarningsEvent struct {
	Symbol         string
	EventTime      time.Time
	Timing         string
	EPSEstimate    *float64
	EPSReported    *float64
	EPSSurprisePct *float64
}

// RawEarnings fetches raw Yahoo earnings-date rows for arbitrary stock tickers.
func RawEarnings(ctx context.Context, pythonBin string, symbols []string, limit int) (map[string][]EarningsEvent, error) {
	clean := make([]string, 0, len(symbols))
	for _, s := range symbols {
		if t := strings.ToUpper(strings.TrimSpace(s)); t != "" {
			clean = append(clean, t)
		}
	}
	if len(clean) == 0 {
		return map[string][]EarningsEvent{}, nil
	}
	if limit <= 0 {
		limit = 12
	}
	raw, err := yfinance.RunFetcher(ctx, pythonBin, "earnings_dates", strings.Join(clean, ","), fmt.Sprintf("%d", limit))
	if err != nil {
		return nil, fmt.Errorf("earnings_dates: %w", err)
	}
	return parseRawEarnings(raw)
}

type rawEarningsRow struct {
	Symbol         string   `json:"symbol"`
	EventTime      string   `json:"event_time"`
	Timing         string   `json:"timing"`
	EPSEstimate    *float64 `json:"eps_estimate"`
	EPSReported    *float64 `json:"eps_reported"`
	EPSSurprisePct *float64 `json:"eps_surprise_pct"`
}

func parseRawEarnings(raw []byte) (map[string][]EarningsEvent, error) {
	var bySymbol map[string][]rawEarningsRow
	if err := json.Unmarshal(raw, &bySymbol); err != nil {
		return nil, fmt.Errorf("parse raw earnings: %w", err)
	}
	out := make(map[string][]EarningsEvent, len(bySymbol))
	for sym, rows := range bySymbol {
		cleanSym := strings.ToUpper(strings.TrimSpace(sym))
		out[cleanSym] = []EarningsEvent{}
		for _, r := range rows {
			ts, err := time.Parse(time.RFC3339, r.EventTime)
			if err != nil {
				continue
			}
			rowSym := strings.ToUpper(strings.TrimSpace(r.Symbol))
			if rowSym == "" {
				rowSym = cleanSym
			}
			out[cleanSym] = append(out[cleanSym], EarningsEvent{
				Symbol:         rowSym,
				EventTime:      ts.UTC(),
				Timing:         r.Timing,
				EPSEstimate:    r.EPSEstimate,
				EPSReported:    r.EPSReported,
				EPSSurprisePct: r.EPSSurprisePct,
			})
		}
	}
	return out, nil
}
