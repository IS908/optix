package shockintel

import "time"

type ShockQuote struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Price     float64   `json:"price"`
	Change    float64   `json:"change"`
	ChangePct float64   `json:"change_pct"`
	Bid       float64   `json:"bid,omitempty"`
	Ask       float64   `json:"ask,omitempty"`
	BidSize   float64   `json:"bid_size,omitempty"`
	AskSize   float64   `json:"ask_size,omitempty"`
	Source    string    `json:"source"`
	Basis     string    `json:"basis"`
	AsOf      time.Time `json:"as_of"`
}

type DepthLevel struct {
	Side  string  `json:"side"`
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
}

type DepthSnapshot struct {
	ID      string       `json:"id"`
	Source  string       `json:"source"`
	Basis   string       `json:"basis"`
	AsOf    time.Time    `json:"as_of"`
	Levels  []DepthLevel `json:"levels"`
	Missing bool         `json:"missing,omitempty"`
	Note    string       `json:"note,omitempty"`
}

type OptionStress struct {
	Underlying string    `json:"underlying"`
	Source     string    `json:"source"`
	Basis      string    `json:"basis"`
	AsOf       time.Time `json:"as_of"`
	IVSkew     float64   `json:"iv_skew"`
	Volume     int64     `json:"volume"`
	OpenInt    int64     `json:"open_interest"`
	Note       string    `json:"note,omitempty"`
}

type RegimeConfirmation struct {
	ID           string    `json:"id"`
	Label        string    `json:"label"`
	Dimension    string    `json:"dimension"`
	ChangePct    float64   `json:"change_pct"`
	Weight       float64   `json:"weight"`
	Contribution float64   `json:"contribution"`
	Source       string    `json:"source"`
	Basis        string    `json:"basis"`
	AsOf         time.Time `json:"as_of"`
	Note         string    `json:"note,omitempty"`
}

type RegimeDTO struct {
	AsOf           time.Time            `json:"as_of"`
	Source         string               `json:"source"`
	State          string               `json:"state"`
	Score          float64              `json:"score"`
	VIXChangeRatio float64              `json:"vix_change_ratio"`
	TriggeredView  string               `json:"triggered_view,omitempty"`
	Confirmations  []RegimeConfirmation `json:"confirmations"`
	Warnings       []string             `json:"warnings,omitempty"`
}

type FingerprintRow struct {
	Kind            string   `json:"kind"`
	Label           string   `json:"label"`
	Score           float64  `json:"score"`
	NormalizedScore float64  `json:"normalized_score"`
	Active          bool     `json:"active"`
	Evidence        []string `json:"evidence"`
	Missing         []string `json:"missing"`
}

type FingerprintDTO struct {
	AsOf         time.Time        `json:"as_of"`
	Source       string           `json:"source"`
	Rows         []FingerprintRow `json:"rows"`
	OptionStress []OptionStress   `json:"option_stress,omitempty"`
	Warnings     []string         `json:"warnings,omitempty"`
}

type ShockVector struct {
	Equity float64 `json:"equity"`
	Vol    float64 `json:"vol"`
	Credit float64 `json:"credit"`
	Rates  float64 `json:"rates"`
	Dollar float64 `json:"dollar"`
	Oil    float64 `json:"oil"`
	Gold   float64 `json:"gold"`
}

type AnalogTemplate struct {
	Name            string
	Date            time.Time
	Category        string
	Vector          ShockVector
	NextSessionBias string
}

type AnalogRow struct {
	Name            string    `json:"name"`
	Date            time.Time `json:"date"`
	Category        string    `json:"category"`
	Similarity      float64   `json:"similarity"`
	NextSessionBias string    `json:"next_session_bias"`
	MatchedFeatures []string  `json:"matched_features"`
}

type AnalogsDTO struct {
	AsOf     time.Time   `json:"as_of"`
	Source   string      `json:"source"`
	Rows     []AnalogRow `json:"rows"`
	Warnings []string    `json:"warnings,omitempty"`
}

type LiquidityRow struct {
	ID             string    `json:"id"`
	Label          string    `json:"label"`
	Source         string    `json:"source"`
	Basis          string    `json:"basis"`
	AsOf           time.Time `json:"as_of"`
	Bid            float64   `json:"bid"`
	Ask            float64   `json:"ask"`
	Mid            float64   `json:"mid"`
	SpreadBps      float64   `json:"spread_bps"`
	SpreadZ        float64   `json:"spread_z"`
	TopBidSize     float64   `json:"top_bid_size"`
	TopAskSize     float64   `json:"top_ask_size"`
	Top5BidDepth   float64   `json:"top5_bid_depth"`
	Top5AskDepth   float64   `json:"top5_ask_depth"`
	DepthAvailable bool      `json:"depth_available"`
	State          string    `json:"state"`
	Missing        bool      `json:"missing,omitempty"`
	Note           string    `json:"note,omitempty"`
}

type LiquidityDTO struct {
	AsOf     time.Time      `json:"as_of"`
	Source   string         `json:"source"`
	Rows     []LiquidityRow `json:"rows"`
	Warnings []string       `json:"warnings,omitempty"`
}

type BundleDTO struct {
	Regime      RegimeDTO      `json:"regime"`
	Fingerprint FingerprintDTO `json:"fingerprint"`
	Analogs     AnalogsDTO     `json:"analogs"`
	Liquidity   LiquidityDTO   `json:"liquidity"`
}
