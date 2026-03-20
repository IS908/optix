package webui

import (
	"time"

	"github.com/IS908/optix/pkg/model"
)

// ─── Watchlist ────────────────────────────────────────────────────────────────

// WatchlistPageResponse is the template payload for GET /watchlist.
type WatchlistPageResponse struct {
	GeneratedAt  time.Time
	Items        []model.WatchlistItem
	FlashError   string
	FlashSuccess string
}

// ─── Dashboard ────────────────────────────────────────────────────────────────

// DashboardResponse is the JSON/template payload for GET /dashboard and /api/dashboard.
type DashboardResponse struct {
	GeneratedAt time.Time               `json:"generated_at"`
	FromCache   bool                    `json:"from_cache"`
	Error       string                  `json:"error,omitempty"` // non-empty = live fetch failed (may still have cached data)
	Symbols     []SymbolSummary         `json:"symbols"`
	Freshness   []model.SymbolFreshness `json:"freshness,omitempty"`
}

// SymbolSummary is one row in the dashboard table.
type SymbolSummary struct {
	Symbol           string  `json:"symbol"`
	Price            float64 `json:"price"`
	Trend            string  `json:"trend"` // "bullish" | "bearish" | "neutral"
	RSI              float64 `json:"rsi"`
	IVRank           float64 `json:"iv_rank"`
	MaxPain          float64 `json:"max_pain"`
	PCR              float64 `json:"pcr"`
	RangeLow1S       float64 `json:"range_low_1s"`
	RangeHigh1S      float64 `json:"range_high_1s"`
	Recommendation   string  `json:"recommendation"`
	OpportunityScore float64 `json:"opportunity_score"`
	SnapshotDate     string  `json:"snapshot_date"`
	NoData           bool    `json:"no_data,omitempty"` // true = no snapshot yet, show "待刷新"
}

// ─── Analyze ─────────────────────────────────────────────────────────────────

// AnalyzeResponse is the JSON/template payload for GET /analyze/{symbol} and /api/analyze/{symbol}.
type AnalyzeResponse struct {
	GeneratedAt time.Time             `json:"generated_at"`
	FromCache   bool                  `json:"from_cache"`
	Symbol      string                `json:"symbol"`
	NoData      bool                  `json:"no_data,omitempty"` // true = no cache, show empty state
	Error       string                `json:"error,omitempty"`   // non-empty = live fetch failed (may still have cached data)
	Summary     SummaryData           `json:"summary"`
	Technical   TechnicalData         `json:"technical"`
	Options     OptionsData           `json:"options"`
	Outlook     OutlookData           `json:"outlook"`
	Strategies  []StrategyData        `json:"strategies"`
	Freshness   model.SymbolFreshness `json:"freshness,omitempty"`
}

type SummaryData struct {
	Price         float64 `json:"price"`
	Change        float64 `json:"change"`
	ChangePct     float64 `json:"change_pct"`
	High52W       float64 `json:"high_52w"`
	Low52W        float64 `json:"low_52w"`
	TodayVolume   int64   `json:"today_volume"`
	AvgVolume20D  float64 `json:"avg_volume_20d"`
}

type PriceLevelData struct {
	Price    float64 `json:"price"`
	Source   string  `json:"source"`
	Strength float64 `json:"strength"`
}

type TechnicalData struct {
	Trend            string           `json:"trend"`
	TrendScore       float64          `json:"trend_score"`
	TrendDescription string           `json:"trend_description"`
	MA20             float64          `json:"ma_20"`
	MA50             float64          `json:"ma_50"`
	MA200            float64          `json:"ma_200"`
	RSI14            float64          `json:"rsi_14"`
	MACD             float64          `json:"macd"`
	MACDSignal       float64          `json:"macd_signal"`
	MACDHistogram    float64          `json:"macd_histogram"`
	BollingerUpper   float64          `json:"bollinger_upper"`
	BollingerMid     float64          `json:"bollinger_mid"`
	BollingerLower   float64          `json:"bollinger_lower"`
	SupportLevels    []PriceLevelData `json:"support_levels"`
	ResistanceLevels []PriceLevelData `json:"resistance_levels"`
}

type OIClusterData struct {
	Strike       float64 `json:"strike"`
	OptionType   string  `json:"option_type"` // "CALL" | "PUT"
	OpenInterest int32   `json:"open_interest"`
	Significance string  `json:"significance"`
}

type OptionsData struct {
	IVCurrent           float64         `json:"iv_current"`
	IVRank              float64         `json:"iv_rank"`
	IVPercentile        float64         `json:"iv_percentile"`
	IVEnvironment       string          `json:"iv_environment"`
	IVSkew              float64         `json:"iv_skew"`
	MaxPain             float64         `json:"max_pain"`
	MaxPainExpiry       string          `json:"max_pain_expiry"`
	PCRVolume           float64         `json:"pcr_volume"`
	PCROi               float64         `json:"pcr_oi"`
	OIClusters          []OIClusterData `json:"oi_clusters"`
	EarningsBeforeExpiry bool           `json:"earnings_before_expiry"`
	NextEarningsDate    string          `json:"next_earnings_date"`
}

type OutlookData struct {
	Direction    string   `json:"direction"`
	Confidence   float64  `json:"confidence"`
	Rationale    string   `json:"rationale"`
	RangeLow1S   float64  `json:"range_low_1s"`
	RangeHigh1S  float64  `json:"range_high_1s"`
	RangeLow2S   float64  `json:"range_low_2s"`
	RangeHigh2S  float64  `json:"range_high_2s"`
	ForecastDays int32    `json:"forecast_days"`
	RiskEvents   []string `json:"risk_events"`
}

type StrategyLegData struct {
	OptionType string  `json:"option_type"` // "CALL" | "PUT"
	Strike     float64 `json:"strike"`
	Expiration string  `json:"expiration"`
	Quantity   int32   `json:"quantity"`
	Premium    float64 `json:"premium"`
}

type StrategyData struct {
	StrategyName        string            `json:"strategy_name"`
	StrategyType        string            `json:"strategy_type"`
	Score               float64           `json:"score"`
	Legs                []StrategyLegData `json:"legs"`
	MaxProfit           float64           `json:"max_profit"`
	MaxLoss             float64           `json:"max_loss"`
	RiskRewardRatio     float64           `json:"risk_reward_ratio"`
	MarginRequired      float64           `json:"margin_required"`
	ProbabilityOfProfit float64           `json:"probability_of_profit"`
	BreakevenPrice      float64           `json:"breakeven_price"`
	NetCredit           float64           `json:"net_credit"`
	Rationale           string            `json:"rationale"`
	RiskWarnings        []string          `json:"risk_warnings"`
}
