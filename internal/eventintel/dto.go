package eventintel

import "time"

type RatePathRow struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Kind      string    `json:"kind"`
	Source    string    `json:"source"`
	Basis     string    `json:"basis"`
	AsOf      time.Time `json:"as_of"`
	PreEvent  float64   `json:"pre_event"`
	Current   float64   `json:"current"`
	Change    float64   `json:"change"`
	ChangePct float64   `json:"change_pct"`
	Missing   bool      `json:"missing,omitempty"`
	Note      string    `json:"note,omitempty"`
}

type RatePathDTO struct {
	AsOf         time.Time     `json:"as_of"`
	Source       string        `json:"source"`
	UniverseNote string        `json:"universe_note"`
	Rows         []RatePathRow `json:"rows"`
	Warnings     []string      `json:"warnings,omitempty"`
}

type StatementFixture struct {
	Title       string    `json:"title"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"published_at"`
	Text        string    `json:"text"`
}

type StatementSentence struct {
	Text   string   `json:"text"`
	Status string   `json:"status"`
	Hits   []string `json:"hits,omitempty"`
}

type StatementDiffDTO struct {
	AsOf               time.Time           `json:"as_of"`
	Source             string              `json:"source"`
	PriorTitle         string              `json:"prior_title"`
	CurrentTitle       string              `json:"current_title"`
	PriorPublishedAt   time.Time           `json:"prior_published_at"`
	CurrentPublishedAt time.Time           `json:"current_published_at"`
	Added              []StatementSentence `json:"added"`
	Removed            []StatementSentence `json:"removed"`
	Unchanged          []StatementSentence `json:"unchanged"`
	HawkishHits        int                 `json:"hawkish_hits"`
	DovishHits         int                 `json:"dovish_hits"`
	Verdict            string              `json:"verdict"`
	Warnings           []string            `json:"warnings,omitempty"`
}

type EventDate struct {
	Date  time.Time `json:"date"`
	Kind  string    `json:"kind"`
	Label string    `json:"label"`
}

type EventWindowReturn struct {
	EventDate    time.Time `json:"event_date"`
	Kind         string    `json:"kind"`
	Label        string    `json:"label"`
	EventMovePct float64   `json:"event_move_pct"`
	NextMovePct  float64   `json:"next_move_pct"`
}

type EventPatternRow struct {
	ID                     string  `json:"id"`
	Label                  string  `json:"label"`
	Source                 string  `json:"source"`
	Basis                  string  `json:"basis"`
	SampleN                int     `json:"sample_n"`
	AvgEventMovePct        float64 `json:"avg_event_move_pct"`
	AvgNextMovePct         float64 `json:"avg_next_move_pct"`
	DirectionalConsistency float64 `json:"directional_consistency"`
	Note                   string  `json:"note,omitempty"`
}

type EventPatternsDTO struct {
	AsOf     time.Time         `json:"as_of"`
	Source   string            `json:"source"`
	Rows     []EventPatternRow `json:"rows"`
	Warnings []string          `json:"warnings,omitempty"`
}

type SensitivityRow struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	RiskOn   float64 `json:"risk_on"`
	RatesUp  float64 `json:"rates_up"`
	DollarUp float64 `json:"dollar_up"`
	SampleN  int     `json:"sample_n"`
	Note     string  `json:"note,omitempty"`
}

type SensitivityDTO struct {
	AsOf     time.Time        `json:"as_of"`
	Source   string           `json:"source"`
	Rows     []SensitivityRow `json:"rows"`
	Warnings []string         `json:"warnings,omitempty"`
}

type BundleDTO struct {
	Rates       RatePathDTO      `json:"rates"`
	Diff        StatementDiffDTO `json:"diff"`
	Patterns    EventPatternsDTO `json:"patterns"`
	Sensitivity SensitivityDTO   `json:"sensitivity"`
}
