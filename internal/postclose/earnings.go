package postclose

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/IS908/optix/internal/marketdata"
)

// BuildEarningsReports filters raw earnings events into the [now-past, now+future]
// window and flags rows older than staleAfter (relative to now) as Stale. past
// must be > staleAfter for the Stale flag to be reachable; the legacy
// past == staleAfter == 14d made Stale structurally unreachable (every row that
// survived the filter was newer than the threshold). #174.2
func BuildEarningsReports(raw map[string][]marketdata.EarningsEvent, now time.Time, past, future, staleAfter time.Duration) []EarningsReport {
	start := now.Add(-past)
	end := now.Add(future)
	staleBefore := now.Add(-staleAfter)
	out := []EarningsReport{}
	for sym, rows := range raw {
		for _, r := range rows {
			if r.EventTime.Before(start) || r.EventTime.After(end) {
				continue
			}
			symbol := strings.ToUpper(strings.TrimSpace(r.Symbol))
			if symbol == "" {
				symbol = strings.ToUpper(strings.TrimSpace(sym))
			}
			out = append(out, EarningsReport{
				Symbol:         symbol,
				Source:         "yfinance",
				Basis:          string(marketdata.BasisDelayed),
				EventTime:      r.EventTime.UTC(),
				Timing:         r.Timing,
				EPSEstimate:    r.EPSEstimate,
				EPSReported:    r.EPSReported,
				EPSSurprisePct: r.EPSSurprisePct,
				SurpriseLabel:  surpriseLabel(r.EPSReported, r.EPSEstimate, r.EPSSurprisePct),
				Stale:          r.EventTime.Before(staleBefore),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].EventTime.Equal(out[j].EventTime) {
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].EventTime.Before(out[j].EventTime)
	})
	return out
}

func surpriseLabel(reported, estimate, surprisePct *float64) string {
	if reported == nil {
		return "scheduled"
	}
	var pct float64
	if surprisePct != nil {
		pct = *surprisePct
	} else if estimate != nil && *estimate != 0 {
		pct = (*reported - *estimate) / math.Abs(*estimate) * 100
	} else {
		return "reported"
	}
	switch {
	case pct >= 2:
		return "beat"
	case pct <= -2:
		return "miss"
	default:
		return "inline"
	}
}
