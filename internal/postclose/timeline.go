package postclose

import (
	"fmt"
	"sort"
	"time"
)

func BuildTimeline(asOf time.Time, reports []EarningsReport, movers []Mover, edges []ReadAcrossEdge) []TimelineEvent {
	events := []TimelineEvent{}
	for _, r := range reports {
		events = append(events, TimelineEvent{
			TS:       r.EventTime,
			Symbol:   r.Symbol,
			Kind:     "earnings",
			Title:    fmt.Sprintf("%s 财报 %s", r.Symbol, r.SurpriseLabel),
			Detail:   earningsDetail(r),
			Severity: severityForSurprise(r.SurpriseLabel),
		})
	}
	for _, m := range movers {
		if abs(m.AfterHoursPct) < 2 {
			continue
		}
		events = append(events, TimelineEvent{
			TS:       asOf,
			Symbol:   m.Symbol,
			Kind:     "mover",
			Title:    fmt.Sprintf("%s 盘后异动 %+.2f%%", m.Symbol, m.AfterHoursPct),
			Detail:   fmt.Sprintf("常规盘 %+.2f%%，全天合并 %+.2f%%", m.RegularPct, m.CombinedPct),
			Severity: severityForMove(m.AfterHoursPct),
		})
	}
	for _, e := range edges {
		events = append(events, TimelineEvent{
			TS:       asOf,
			Symbol:   e.Driver,
			Kind:     "read_across",
			Title:    fmt.Sprintf("%s -> %s read-across", e.Driver, e.Peer),
			Detail:   e.Note,
			Severity: "info",
		})
	}
	// Newest-first: postclose is a "what just happened" view, and the cap below
	// trims the tail (oldest), so today's mover/read-across rows (TS=asOf) always
	// survive while the OLDEST earnings rows are dropped. #174.1 — the SPA
	// renders events.slice(0, 8), so this order also drives what users see first.
	sort.SliceStable(events, func(i, j int) bool { return events[i].TS.After(events[j].TS) })
	if len(events) > 16 {
		return events[:16]
	}
	return events
}

func earningsDetail(r EarningsReport) string {
	if r.EPSReported != nil && r.EPSEstimate != nil {
		return fmt.Sprintf("EPS %.2f vs %.2f", *r.EPSReported, *r.EPSEstimate)
	}
	if r.EPSEstimate != nil {
		return fmt.Sprintf("EPS consensus %.2f", *r.EPSEstimate)
	}
	return "EPS consensus unavailable"
}

func severityForSurprise(label string) string {
	switch label {
	case "beat":
		return "positive"
	case "miss":
		return "negative"
	default:
		return "info"
	}
}

func severityForMove(pct float64) string {
	if pct >= 0 {
		return "positive"
	}
	return "negative"
}
