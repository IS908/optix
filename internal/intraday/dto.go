package intraday

import "time"

type Quote struct {
	Symbol string
	Source string
	Basis  string
	Last   float64
	Bid    float64
	Ask    float64
	Close  float64
	Volume int64
	AsOf   time.Time
}

type Mover struct {
	Symbol    string    `json:"symbol"`
	Source    string    `json:"source"`
	Basis     string    `json:"basis"`
	AsOf      time.Time `json:"as_of"`
	Last      float64   `json:"last"`
	Open      float64   `json:"open"`
	Pct       float64   `json:"pct"`
	Volume    int64     `json:"volume"`
	Watchlist bool      `json:"watchlist"`
}

type MoversDTO struct {
	AsOf         time.Time `json:"as_of"`
	Source       string    `json:"source"`
	Basis        string    `json:"basis"`
	UniverseNote string    `json:"universe_note"`
	Gainers      []Mover   `json:"gainers"`
	Losers       []Mover   `json:"losers"`
	Warnings     []string  `json:"warnings,omitempty"`
}

type SectorHeatmapRow struct {
	SectorID    string  `json:"sector_id"`
	SectorLabel string  `json:"sector_label"`
	AvgPct      float64 `json:"avg_pct"`
	SampleN     int     `json:"sample_n"`
	Gainers     int     `json:"gainers"`
	Losers      int     `json:"losers"`
	TopSymbol   string  `json:"top_symbol"`
}

type SectorHeatmapDTO struct {
	AsOf         time.Time          `json:"as_of"`
	Source       string             `json:"source"`
	Basis        string             `json:"basis"`
	SectorSource string             `json:"sector_source"`
	Rows         []SectorHeatmapRow `json:"rows"`
	Warnings     []string           `json:"warnings,omitempty"`
}
