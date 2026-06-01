package model

import "time"

// RoundTrip is one open→close position lifecycle, computed by FIFO matching.
// Status: "open" (not closed), "closed" (fully matched), "expired" (option
// past its expiration without closing).
type RoundTrip struct {
	Symbol        string    `json:"symbol"`
	SecType       string    `json:"sec_type"`
	Expiration    string    `json:"expiration,omitempty"`
	Strike        float64   `json:"strike,omitempty"`
	Right         string    `json:"right,omitempty"`
	Account       string    `json:"account"`
	Currency      string    `json:"currency"`
	Direction     string    `json:"direction"` // LONG | SHORT
	OpenTime      time.Time `json:"open_time"`
	CloseTime     time.Time `json:"close_time,omitempty"`
	OpenQty       float64   `json:"open_qty"`
	CloseQty      float64   `json:"close_qty"`
	OpenAvgPrice  float64   `json:"open_avg_price"`
	CloseAvgPrice float64   `json:"close_avg_price"`
	Multiplier    float64   `json:"multiplier"`
	RealizedPnL   float64   `json:"realized_pnl"`
	HoldingDays   float64   `json:"holding_days"`
	Status        string    `json:"status"`
	OpenExecIDs   []string  `json:"open_exec_ids"`
	CloseExecIDs  []string  `json:"close_exec_ids"`
}

func (r RoundTrip) IsOption() bool { return r.SecType == "OPT" }

// SymbolStats is the per-symbol roll-up used inside ReviewSummary.BySymbol.
type SymbolStats struct {
	Symbol         string  `json:"symbol"`
	SecType        string  `json:"sec_type"`
	Currency       string  `json:"currency"`
	RoundTripCount int     `json:"round_trip_count"`
	RealizedPnL    float64 `json:"realized_pnl"`
	WinRate        float64 `json:"win_rate"`
}

// ReviewSummary aggregates round-trip statistics over a time window.
type ReviewSummary struct {
	Since             time.Time `json:"since"`
	Until             time.Time `json:"until"`
	TotalExecutions   int       `json:"total_executions"`
	TotalRoundTrips   int       `json:"total_round_trips"`
	ClosedRoundTrips  int       `json:"closed_round_trips"`
	OpenRoundTrips    int       `json:"open_round_trips"`
	ExpiredRoundTrips int       `json:"expired_round_trips"`
	// ExcludedNonUSDRoundTrips counts trips omitted from USD-only P&L summary
	// metrics because no FX conversion is applied in the journal.
	ExcludedNonUSDRoundTrips int     `json:"excluded_non_usd_round_trips,omitempty"`
	WinCount                 int     `json:"win_count"`
	LossCount                int     `json:"loss_count"`
	WinRate                  float64 `json:"win_rate"`
	TotalRealizedPnL         float64 `json:"total_realized_pnl"`
	AvgRealizedPnL           float64 `json:"avg_realized_pnl"`
	AvgHoldingDays           float64 `json:"avg_holding_days"`
	// BySymbol is sorted by RealizedPnL descending.
	BySymbol []SymbolStats `json:"by_symbol"`
}
