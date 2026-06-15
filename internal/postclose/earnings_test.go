package postclose

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/IS908/optix/internal/marketdata"
)

func fptr(v float64) *float64 { return &v }

func TestBuildEarningsReportsFiltersWindowAndClassifies(t *testing.T) {
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	raw := map[string][]marketdata.EarningsEvent{
		"AAPL": {
			{
				Symbol:         "AAPL",
				EventTime:      now.Add(-24 * time.Hour),
				Timing:         "postmarket",
				EPSEstimate:    fptr(1.00),
				EPSReported:    fptr(1.08),
				EPSSurprisePct: fptr(8.0),
			},
			{Symbol: "AAPL", EventTime: now.Add(-45 * 24 * time.Hour), EPSEstimate: fptr(1.0)},
		},
		"MSFT": {
			{Symbol: "MSFT", EventTime: now.Add(7 * 24 * time.Hour), Timing: "postmarket", EPSEstimate: fptr(2.40)},
		},
	}

	got := BuildEarningsReports(raw, now, 30*24*time.Hour, 30*24*time.Hour, 14*24*time.Hour)
	if len(got) != 2 {
		t.Fatalf("reports = %d, want 2: %+v", len(got), got)
	}
	if got[0].Symbol != "AAPL" || got[0].SurpriseLabel != "beat" || got[0].Stale {
		t.Fatalf("first report = %+v", got[0])
	}
	if got[1].Symbol != "MSFT" || got[1].SurpriseLabel != "scheduled" {
		t.Fatalf("second report = %+v", got[1])
	}
	var row map[string]any
	if err := json.Unmarshal(mustJSON(t, got[0]), &row); err != nil {
		t.Fatal(err)
	}
	if row["source"] != "yfinance" || row["basis"] != "delayed" {
		t.Fatalf("source/basis = %v/%v, want yfinance/delayed; row=%+v", row["source"], row["basis"], row)
	}
}

// TestBuildEarningsReportsMarksOldEventsStale pins #174.2: previously the
// inclusion window's lower bound (now-14d) equalled the Stale threshold (also
// now-14d), so the Stale flag was structurally unreachable. With a 30d
// inclusion window and a 14d stale threshold, events between -30d and -14d
// must appear in the output with Stale=true.
func TestBuildEarningsReportsMarksOldEventsStale(t *testing.T) {
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	raw := map[string][]marketdata.EarningsEvent{
		"FRESH": {{Symbol: "FRESH", EventTime: now.Add(-3 * 24 * time.Hour)}},
		"STALE": {{Symbol: "STALE", EventTime: now.Add(-20 * 24 * time.Hour)}},
	}
	got := BuildEarningsReports(raw, now, 30*24*time.Hour, 30*24*time.Hour, 14*24*time.Hour)
	if len(got) != 2 {
		t.Fatalf("reports = %d, want 2: %+v", len(got), got)
	}
	bySym := map[string]EarningsReport{}
	for _, r := range got {
		bySym[r.Symbol] = r
	}
	if r := bySym["FRESH"]; r.Stale {
		t.Errorf("FRESH report (-3d) should not be stale, got %+v", r)
	}
	if r, ok := bySym["STALE"]; !ok || !r.Stale {
		t.Errorf("STALE report (-20d) must appear and be marked stale, got ok=%v report=%+v", ok, r)
	}
}

func TestSurpriseLabelFallsBackToReportedVsEstimate(t *testing.T) {
	if got := surpriseLabel(fptr(0.98), fptr(1.00), nil); got != "miss" {
		t.Fatalf("miss label = %q", got)
	}
	if got := surpriseLabel(fptr(1.01), fptr(1.00), nil); got != "inline" {
		t.Fatalf("inline label = %q", got)
	}
}

func TestSurpriseLabelUsesAbsoluteNegativeEstimate(t *testing.T) {
	if got := surpriseLabel(fptr(-0.90), fptr(-1.00), nil); got != "beat" {
		t.Fatalf("negative estimate beat label = %q", got)
	}
	if got := surpriseLabel(fptr(-1.10), fptr(-1.00), nil); got != "miss" {
		t.Fatalf("negative estimate miss label = %q", got)
	}
}
