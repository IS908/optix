package postclose

import (
	"testing"
	"time"
)

func TestBuildTimelineCombinesAndOrdersEvents(t *testing.T) {
	// Postclose timeline is "what just happened" → newest events lead. Mover and
	// read_across rows are stamped TS=asOf; the earnings row is 1h earlier, so it
	// comes last.
	asOf := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	reports := []EarningsReport{
		{Symbol: "AAPL", EventTime: asOf.Add(-1 * time.Hour), SurpriseLabel: "beat"},
	}
	movers := []Mover{
		{Symbol: "AAPL", AfterHoursPct: 3.2, CombinedPct: 5.0},
	}
	edges := []ReadAcrossEdge{
		{Driver: "AAPL", Peer: "MSFT", SectorLabel: "Mega-cap Tech", Direction: "positive"},
	}

	got := BuildTimeline(asOf, reports, movers, edges)
	if len(got) != 3 {
		t.Fatalf("events = %+v", got)
	}
	// Newest-first ordering; the two asOf-stamped events come before the
	// 1h-earlier earnings row. Their relative order is preserved by stable sort
	// (mover appended before read_across in the builder).
	if got[0].Kind != "mover" {
		t.Fatalf("first event = %+v (want mover)", got[0])
	}
	if got[1].Kind != "read_across" {
		t.Fatalf("second event = %+v (want read_across)", got[1])
	}
	if got[2].Kind != "earnings" || got[2].Symbol != "AAPL" {
		t.Fatalf("third event = %+v (want earnings AAPL)", got[2])
	}
}

// TestBuildTimelineTruncatesOldestNotNewest pins #174.1: ascending sort + [:16]
// kept the OLDEST 16 events, so today's mover/read-across (the whole point of a
// postclose "what just happened" timeline) was dropped when older earnings rows
// pushed the count past 16. The new contract is newest-first + cap.
func TestBuildTimelineTruncatesOldestNotNewest(t *testing.T) {
	asOf := time.Date(2026, 6, 13, 16, 30, 0, 0, time.UTC)
	// 16 stale earnings spread across the prior two weeks.
	reports := make([]EarningsReport, 0, 16)
	for i := 0; i < 16; i++ {
		reports = append(reports, EarningsReport{
			Symbol:        "OLD",
			EventTime:     asOf.Add(time.Duration(-i-1) * 24 * time.Hour),
			SurpriseLabel: "beat",
		})
	}
	// 2 same-day events (TS == asOf) that must survive the truncation.
	movers := []Mover{{Symbol: "TODAY", AfterHoursPct: 4.2, CombinedPct: 5.0}}
	edges := []ReadAcrossEdge{{Driver: "TODAY", Peer: "PEER", Direction: "positive"}}

	got := BuildTimeline(asOf, reports, movers, edges)

	if len(got) != 16 {
		t.Fatalf("expected truncation to 16, got %d", len(got))
	}
	// Both same-day events must be present.
	var sawMover, sawReadAcross bool
	for _, e := range got {
		if e.Kind == "mover" && e.Symbol == "TODAY" {
			sawMover = true
		}
		if e.Kind == "read_across" && e.Symbol == "TODAY" {
			sawReadAcross = true
		}
	}
	if !sawMover {
		t.Errorf("today's mover dropped by truncation; events=%+v", summaries(got))
	}
	if !sawReadAcross {
		t.Errorf("today's read-across dropped by truncation; events=%+v", summaries(got))
	}
	// Newest-first invariant: the first event has the largest TS.
	for i := 1; i < len(got); i++ {
		if got[i].TS.After(got[0].TS) {
			t.Errorf("got[%d].TS (%v) is after got[0].TS (%v); sort is not descending",
				i, got[i].TS, got[0].TS)
		}
	}
}

func summaries(evts []TimelineEvent) []string {
	out := make([]string, len(evts))
	for i, e := range evts {
		out[i] = e.Kind + ":" + e.Symbol
	}
	return out
}
