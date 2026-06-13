package postclose

import (
	"testing"
	"time"
)

func TestBuildTimelineCombinesAndOrdersEvents(t *testing.T) {
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
	if got[0].Kind != "earnings" || got[0].Symbol != "AAPL" {
		t.Fatalf("first event = %+v", got[0])
	}
	if got[1].Kind != "mover" {
		t.Fatalf("second event = %+v", got[1])
	}
	if got[2].Kind != "read_across" {
		t.Fatalf("third event = %+v", got[2])
	}
}
