package model

import "time"

// Position is one account holding. Identity + cost fields come straight from
// IBKR's ReqPositions; the mark-to-market fields are filled by AccountService.
type Position struct {
	Account    string
	Symbol     string
	SecType    string  // "STK" | "OPT"
	Expiration string  // option expiry "YYYYMMDD"; empty for stock
	Strike     float64 // option strike; 0 for stock
	Right      string  // option "C" | "P"; empty for stock
	Quantity   float64 // signed — negative is a short position; may be fractional
	AvgCost    float64 // IBKR convention: per-share for STK, per-contract
	// (multiplier already baked in) for OPT
	Multiplier float64 // 1 for STK, usually 100 for OPT
	Currency   string  // IBKR contract currency, e.g. "USD" | "HKD" | "SGD".
	// Empty when the source didn't provide it (treated as
	// USD by USD-only consumers like portfolio concentration).

	// Filled by AccountService from a mark price. Left zero (CLI shows "—")
	// when the mark is unavailable, e.g. an option with no OPRA subscription.
	LastPrice        float64
	MarketValue      float64
	UnrealizedPnL    float64
	UnrealizedPnLPct float64
}

// IsOption reports whether this position is an option contract.
func (p Position) IsOption() bool { return p.SecType == "OPT" }

// Execution is one fill from the last-7-days execution history.
type Execution struct {
	ExecID     string
	Time       time.Time
	Account    string
	Symbol     string
	SecType    string
	Expiration string // option fields; empty/zero for stock
	Strike     float64
	Right      string
	Side       string // "BOT" | "SLD"
	Shares     float64
	Price      float64
	AvgPrice   float64
	Currency   string // IBKR contract currency. Empty legacy rows are treated as USD.
	Exchange   string
	OrderID    int64
	PermID     int64
}

// ExecutionFilter narrows an execution-history query. All fields are optional.
type ExecutionFilter struct {
	Symbol string
	Side   string    // "BOT" | "SLD"
	Since  time.Time // must fall within the last 7 days
}
