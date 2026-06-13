package model

import "time"

// PremarketGapStat 是某标的、某方向、某幅度档的历史同日跳空回补统计（M4 跳空卡）。
// json tag = 线上契约（CLI 与 HTTP 共用）。
type PremarketGapStat struct {
	Symbol       string    `json:"symbol"`
	Direction    string    `json:"direction"` // up | down
	Band         string    `json:"band"`      // "0.25-0.5" | "0.5-1" | "1+"
	FillRate     float64   `json:"fill_rate"` // [0,1]
	SampleN      int       `json:"sample_n"`
	LookbackDays int       `json:"lookback_days"`
	AsOf         time.Time `json:"as_of"`
}
