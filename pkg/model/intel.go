package model

import "time"

// IntelNarrative 是一条检查点叙事条目（展示，不打分）。json tag = 线上契约。
type IntelNarrative struct {
	EntryID     string    `json:"entry_id"`
	TradingDate string    `json:"trading_date"`
	Checkpoint  string    `json:"checkpoint"`
	Phase       string    `json:"phase"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
}

// IntelReconciliation 是一条判断的持久化结算。
type IntelReconciliation struct {
	JudgmentID  string    `json:"judgment_id"`
	ExpiryPrice float64   `json:"expiry_price"`
	ExpiryBasis string    `json:"expiry_basis"`
	Outcome     string    `json:"outcome"` // hit|miss|push|void
	DeltaPct    float64   `json:"delta_pct"`
	SettledAt   time.Time `json:"settled_at"`
}

// IntelJudgment 是一条结构化方向判断（可对账）。Reconciliation 读时内联（存储不嵌）。
type IntelJudgment struct {
	JudgmentID       string               `json:"judgment_id"`
	TradingDate      string               `json:"trading_date"`
	Checkpoint       string               `json:"checkpoint"`
	AssetID          string               `json:"asset_id"`
	AssetClass       string               `json:"asset_class"`
	Direction        string               `json:"direction"` // up|down|flat
	ThresholdPct     float64              `json:"threshold_pct"`
	Confidence       int                  `json:"confidence"`
	ExpiryCheckpoint string               `json:"expiry_checkpoint"`
	ExpiryAt         time.Time            `json:"expiry_at"`
	RegisteredPrice  float64              `json:"registered_price"`
	RegisteredBasis  string               `json:"registered_basis"`
	Rationale        string               `json:"rationale,omitempty"`
	Supersedes       string               `json:"supersedes,omitempty"`
	CreatedAt        time.Time            `json:"created_at"`
	Reconciliation   *IntelReconciliation `json:"reconciliation,omitempty"`
}
