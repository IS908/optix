package premarket

import (
	"time"

	"github.com/IS908/optix/pkg/model"
)

type OvernightLink struct {
	ID    string    `json:"id"`
	Label string    `json:"label"`
	Pct   float64   `json:"pct"`
	Basis string    `json:"basis"`
	AsOf  time.Time `json:"as_of"`
}

type Consistency struct {
	SameDir int    `json:"same_dir"`
	Total   int    `json:"total"`
	Note    string `json:"note"`
}

type OvernightDTO struct {
	AsOf        time.Time       `json:"as_of"`
	Links       []OvernightLink `json:"links"`
	Consistency Consistency     `json:"consistency"`
	Warnings    []string        `json:"warnings,omitempty"`
}

type GapsDTO struct {
	Symbol        string                   `json:"symbol"`
	ImpliedGapPct float64                  `json:"implied_gap_pct"`
	Direction     string                   `json:"direction"`
	Band          string                   `json:"band"`
	HistFillRate  float64                  `json:"hist_fill_rate"`
	SampleN       int                      `json:"sample_n"`
	LookbackDays  int                      `json:"lookback_days"`
	ByBand        []model.PremarketGapStat `json:"by_band"`
	AsOf          time.Time                `json:"as_of"`
	Warnings      []string                 `json:"warnings,omitempty"`
}

type Mover struct {
	Symbol    string  `json:"symbol"`
	Pct       float64 `json:"pct"`
	VolRatio  float64 `json:"vol_ratio"`
	Watchlist bool    `json:"watchlist"`
}

type MoversDTO struct {
	AsOf         time.Time `json:"as_of"`
	UniverseNote string    `json:"universe_note"`
	Gainers      []Mover   `json:"gainers"`
	Losers       []Mover   `json:"losers"`
	Warnings     []string  `json:"warnings,omitempty"`
}

type SentimentDTO struct {
	AsOf           time.Time `json:"as_of"`
	PCOI           float64   `json:"pc_oi"`
	PCVol          float64   `json:"pc_vol"`
	PCAvailable    bool      `json:"pc_available"`
	VIX            float64   `json:"vix"`
	VIX3M          float64   `json:"vix3m"`
	VIXTermPremium float64   `json:"vix_term_premium"`
	Regime         string    `json:"regime"`
	DegradedNote   string    `json:"degraded_note"`
	Warnings       []string  `json:"warnings,omitempty"`
}

type BundleDTO struct {
	Overnight OvernightDTO `json:"overnight"`
	Gaps      GapsDTO      `json:"gaps"`
	Movers    MoversDTO    `json:"movers"`
	Sentiment SentimentDTO `json:"sentiment"`
}
