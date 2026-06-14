package eventintel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFedSourceEventDatesFromStatementLinks(t *testing.T) {
	srv := fedTestServer(t)
	defer srv.Close()

	src := NewFedSource(srv.URL + "/monetarypolicy/fomccalendars.htm")
	events, err := src.EventDates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2: %#v", len(events), events)
	}
	if events[0].Kind != "FOMC" || events[0].Date != dateUTC(2026, 3, 18) {
		t.Fatalf("first event = %#v", events[0])
	}
	if events[1].Label != "2026 Apr 29 FOMC" {
		t.Fatalf("second label = %q", events[1].Label)
	}
}

func TestFedSourceStatementsFetchesLatestTwoHTMLStatements(t *testing.T) {
	srv := fedTestServer(t)
	defer srv.Close()

	src := NewFedSource(srv.URL + "/monetarypolicy/fomccalendars.htm")
	prior, current, err := src.Statements(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prior.PublishedAt != time.Date(2026, 3, 18, 18, 0, 0, 0, time.UTC) {
		t.Fatalf("prior published = %s", prior.PublishedAt)
	}
	if current.PublishedAt != time.Date(2026, 4, 29, 18, 0, 0, 0, time.UTC) {
		t.Fatalf("current published = %s", current.PublishedAt)
	}
	if current.Source != "fed.gov FOMC statement" {
		t.Fatalf("current source = %q", current.Source)
	}
	if !strings.Contains(current.Text, "Inflation remains elevated") {
		t.Fatalf("current text missing statement body: %q", current.Text)
	}
	if strings.Contains(current.Text, "For media inquiries") {
		t.Fatalf("current text should exclude page boilerplate: %q", current.Text)
	}
}

func TestYFinanceAdapterUsesFedStatementsAndMergesFedEventDates(t *testing.T) {
	srv := fedTestServer(t)
	defer srv.Close()

	adapter := &YFinanceAdapter{fed: NewFedSource(srv.URL + "/monetarypolicy/fomccalendars.htm")}
	prior, current, err := adapter.Statements(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prior.Source != "fed.gov FOMC statement" || current.Source != "fed.gov FOMC statement" {
		t.Fatalf("statement sources = %q/%q", prior.Source, current.Source)
	}
	events, err := adapter.EventDates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "FOMC", dateUTC(2026, 4, 29)) {
		t.Fatalf("missing Fed FOMC event in %#v", events)
	}
	if !hasEvent(events, "CPI", dateUTC(2026, 6, 10)) {
		t.Fatalf("local CPI events should remain merged in %#v", events)
	}
}

func TestYFinanceAdapterFallsBackToLocalFixturesWithWarning(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	adapter := &YFinanceAdapter{fed: NewFedSource(srv.URL + "/missing")}
	prior, current, err := adapter.Statements(context.Background())
	if err == nil {
		t.Fatal("expected statements warning error")
	}
	if prior.Source != "local_statement_fixture" || current.Source != "local_statement_fixture" {
		t.Fatalf("fallback statement sources = %q/%q", prior.Source, current.Source)
	}
	events, err := adapter.EventDates(context.Background())
	if err == nil {
		t.Fatal("expected event calendar warning error")
	}
	if !hasEvent(events, "CPI", dateUTC(2026, 6, 10)) {
		t.Fatalf("fallback events missing local calendar: %#v", events)
	}
}

func hasEvent(events []EventDate, kind string, date time.Time) bool {
	for _, event := range events {
		if event.Kind == kind && event.Date.Equal(date) {
			return true
		}
	}
	return false
}

func fedTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/monetarypolicy/fomccalendars.htm", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`
<html><body>
<h4>March</h4>
<a href="/newsevents/pressreleases/monetary20260318a.htm">HTML</a>
<h4>April</h4>
<a href="/newsevents/pressreleases/monetary20260429a.htm">HTML</a>
</body></html>`))
	})
	mux.HandleFunc("/newsevents/pressreleases/monetary20260318a.htm", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(statementHTML("March 18, 2026", "Recent indicators suggest growth is solid.", "Inflation has eased but remains somewhat elevated.")))
	})
	mux.HandleFunc("/newsevents/pressreleases/monetary20260429a.htm", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(statementHTML("April 29, 2026", "Recent indicators suggest growth is solid.", "Inflation remains elevated.")))
	})
	return httptest.NewServer(mux)
}

func statementHTML(date, p1, p2 string) string {
	return `
<html><body>
<main>
<div>` + date + `</div>
<h3>Federal Reserve issues FOMC statement</h3>
<p>For release at 2:00 p.m. EDT</p>
<p>` + p1 + `</p>
<p>` + p2 + `</p>
<p>For media inquiries, please email media@example.com.</p>
</main>
</body></html>`
}
