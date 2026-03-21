package model

import "time"

// SymbolFreshness records the last successful IBKR data-fetch timestamp for
// each storage layer of a single symbol. A zero Time means no data exists yet.
type SymbolFreshness struct {
	Symbol     string    `json:"symbol"`
	QuoteAt    time.Time `json:"quote_at"`    // stock_quotes.updated_at
	OHLCVAt    time.Time `json:"ohlcv_at"`    // newest ohlcv_bars.open_time (1D)
	OptionsAt  time.Time `json:"options_at"`  // newest option_quotes.snapshot_time
	CacheAt    time.Time `json:"cache_at"`    // analysis_cache.cached_at
	SnapshotAt time.Time `json:"snapshot_at"` // newest watchlist_snapshots.snapshot_date
}

// WatchlistItem represents a stock in the user's watchlist.
type WatchlistItem struct {
	Symbol                 string
	AddedAt                string // ISO8601
	Notes                  string
	Tags                   []string
	AutoRefreshEnabled     bool
	RefreshIntervalMinutes int
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
	SnapshotDate     string  // YYYY-MM-DD, populated when reading from DB
}

// SymbolRefresh represents a symbol that needs background refresh.
type SymbolRefresh struct {
	Symbol      string
	Interval    int       // Refresh interval in minutes
	LastRefresh time.Time // Last successful refresh timestamp
}

// BackgroundJob represents a background task execution record.
type BackgroundJob struct {
	ID           int64
	Symbol       string
	JobType      string // "analyze"
	Status       string // "pending", "running", "success", "failed"
	StartedAt    *time.Time
	CompletedAt  *time.Time
	ErrorMessage string
	RetryCount   int
	CreatedAt    time.Time
}
