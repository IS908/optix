package intel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
)

func TestJournalEndpoint(t *testing.T) {
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := dET(2026, 6, 12, 11, 0)
	jrnl := NewIntelJournal(s, fakePrice{price: 100, basis: "delayed"})
	jrnl.Now = func() time.Time { return now }
	if _, err := jrnl.WriteNarrative(context.Background(), "script", "剧本"); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{Journal: jrnl, Now: func() time.Time { return now }}
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/intel/journal", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["trading_date"] != "2026-06-12" {
		t.Errorf("trading_date = %v", body["trading_date"])
	}
	if len(body["checkpoints"].([]any)) != 4 {
		t.Errorf("want 4 checkpoints")
	}
	if len(body["narratives"].([]any)) != 1 {
		t.Errorf("want 1 narrative")
	}
}

func TestJournalEndpointNilJournal(t *testing.T) {
	h := &Handlers{Now: func() time.Time { return dET(2026, 6, 12, 11, 0) }}
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/intel/journal", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil journal → want 503, got %d", rec.Code)
	}
}
