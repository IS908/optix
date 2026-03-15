package model

// WatchlistItem represents a stock in the user's watchlist.
type WatchlistItem struct {
	Symbol  string
	AddedAt string // ISO8601
	Notes   string
	Tags    []string
}

// PriceLevel represents a support or resistance level.
type PriceLevel struct {
	Price    float64
	Source   string  // "ma_50", "fib_0.382", "oi_wall", "max_pain", etc.
	Strength float64 // 0-100
}

// QuickSummary is a compact summary for the watchlist dashboard.
type QuickSummary struct {
	Symbol           string
	Price            float64
	Trend            string  // "bullish" / "bearish" / "neutral"
	RSI              float64
	IVRank           float64 // 0-100
	MaxPain          float64
	PCR              float64
	RangeLow1S       float64
	RangeHigh1S      float64
	Recommendation   string
	OpportunityScore float64 // 0-100
}
