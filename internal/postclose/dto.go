package postclose

import "time"

type EarningsReport struct {
	Symbol         string    `json:"symbol"`
	Source         string    `json:"source"`
	Basis          string    `json:"basis"`
	EventTime      time.Time `json:"event_time"`
	Timing         string    `json:"timing"`
	EPSEstimate    *float64  `json:"eps_estimate,omitempty"`
	EPSReported    *float64  `json:"eps_reported,omitempty"`
	EPSSurprisePct *float64  `json:"eps_surprise_pct,omitempty"`
	SurpriseLabel  string    `json:"surprise_label"`
	Stale          bool      `json:"stale"`
}

type EarningsDTO struct {
	AsOf         time.Time        `json:"as_of"`
	Source       string           `json:"source"`
	UniverseNote string           `json:"universe_note"`
	Reports      []EarningsReport `json:"reports"`
	Warnings     []string         `json:"warnings,omitempty"`
}

type TimelineEvent struct {
	TS       time.Time `json:"ts"`
	Symbol   string    `json:"symbol,omitempty"`
	Kind     string    `json:"kind"`
	Title    string    `json:"title"`
	Detail   string    `json:"detail"`
	Severity string    `json:"severity"`
}

type TimelineDTO struct {
	AsOf     time.Time       `json:"as_of"`
	Events   []TimelineEvent `json:"events"`
	Warnings []string        `json:"warnings,omitempty"`
}

type ReadAcrossEdge struct {
	Driver              string  `json:"driver"`
	Peer                string  `json:"peer"`
	SectorID            string  `json:"sector_id"`
	SectorLabel         string  `json:"sector_label"`
	Direction           string  `json:"direction"`
	SignalStrength      float64 `json:"signal_strength"`
	Lag                 string  `json:"lag"`
	DriverAfterHoursPct float64 `json:"driver_after_hours_pct"`
	Note                string  `json:"note"`
}

type ReadAcrossDTO struct {
	AsOf         time.Time        `json:"as_of"`
	SectorSource string           `json:"sector_source"`
	Edges        []ReadAcrossEdge `json:"edges"`
	Warnings     []string         `json:"warnings,omitempty"`
}

type Mover struct {
	Symbol        string  `json:"symbol"`
	Source        string  `json:"source"`
	Basis         string  `json:"basis"`
	RegularPct    float64 `json:"regular_pct"`
	AfterHoursPct float64 `json:"after_hours_pct"`
	CombinedPct   float64 `json:"combined_pct"`
	Volume        int64   `json:"volume"`
	Watchlist     bool    `json:"watchlist"`
}

type MoversDTO struct {
	AsOf         time.Time `json:"as_of"`
	UniverseNote string    `json:"universe_note"`
	Gainers      []Mover   `json:"gainers"`
	Losers       []Mover   `json:"losers"`
	Warnings     []string  `json:"warnings,omitempty"`
}

type BundleDTO struct {
	Earnings   EarningsDTO   `json:"earnings"`
	Timeline   TimelineDTO   `json:"timeline"`
	ReadAcross ReadAcrossDTO `json:"read_across"`
	Movers     MoversDTO     `json:"movers"`
}
