package eventintel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBLSSourceEventDatesFromCPISchedule(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`
<html><body>
<h1>Schedule of Releases for the Consumer Price Index</h1>
<table>
<tr><th>Reference Month</th><th>Release Date</th><th>Release Time</th></tr>
<tr><td>May 2026</td><td>Jun. 10, 2026</td><td>08:30 AM</td></tr>
<tr><td>June 2026</td><td>Jul. 14, 2026</td><td>08:30 AM</td></tr>
</table>
</body></html>`))
	}))
	defer srv.Close()

	src := NewBLSSource(srv.URL)
	events, err := src.EventDates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2: %#v", len(events), events)
	}
	if events[0].Kind != "CPI" || events[0].Date != dateUTC(2026, 6, 10) || events[0].Label != "2026 May CPI" {
		t.Fatalf("first event = %#v", events[0])
	}
	if events[1].Date != dateUTC(2026, 7, 14) || events[1].Label != "2026 Jun CPI" {
		t.Fatalf("second event = %#v", events[1])
	}
}
