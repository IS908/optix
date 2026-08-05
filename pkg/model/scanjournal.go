package model

import "time"

// ScanCandidate 是 sell-put 扫描的开仓快照（追加式，永不 UPDATE）。json tag = 线上契约。
type ScanCandidate struct {
	CandidateID        string    `json:"candidate_id"`
	ScanDate           string    `json:"scan_date"` // NY 交易日 2006-01-02
	Rank               int       `json:"rank"`
	Symbol             string    `json:"symbol"`
	Right              string    `json:"right"` // 'P'（预留 covered call）
	Expiry             string    `json:"expiry"`
	DTE                int       `json:"dte"`
	Strike             float64   `json:"strike"`
	Spot               float64   `json:"spot"`
	Bid                float64   `json:"bid"`
	Ask                *float64  `json:"ask,omitempty"`
	Mid                *float64  `json:"mid,omitempty"`
	IV                 *float64  `json:"iv,omitempty"`
	Delta              *float64  `json:"delta,omitempty"`
	OI                 *int      `json:"oi,omitempty"`
	Volume             *int      `json:"volume,omitempty"`
	CushionPct         float64   `json:"cushion_pct"`
	PremiumYieldPct    float64   `json:"premium_yield_pct"`
	AnnualizedYieldPct float64   `json:"annualized_yield_pct"`
	Score              float64   `json:"score"`
	IBKRBid            *float64  `json:"ibkr_bid,omitempty"`
	IBKRAsk            *float64  `json:"ibkr_ask,omitempty"`
	IBKROptionIV       *float64  `json:"ibkr_option_iv,omitempty"`
	IBKROptionDelta    *float64  `json:"ibkr_option_delta,omitempty"`
	SymbolSource       string    `json:"symbol_source"`
	CreatedAt          time.Time `json:"created_at"`
}

// ScanReconciliation 是一条候选的到期对账结果（candidate_id 一对一）。
type ScanReconciliation struct {
	CandidateID  string    `json:"candidate_id"`
	ExpiryClose  float64   `json:"expiry_close"` // void 时 0
	Outcome      string    `json:"outcome"`      // hit|miss|void
	RealizedPnL  float64   `json:"realized_pnl"` // 每股；void 时 0
	Touched      bool      `json:"touched"`
	MaxBreachPct float64   `json:"max_breach_pct"`
	ExpiryBasis  string    `json:"expiry_basis"` // delayed（yfinance 日线）
	SettledAt    time.Time `json:"settled_at"`
}
