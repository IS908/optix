// Package webui — trade-journal HTTP handlers.
//
// Read paths (list, trips, review) are served from SQLite and never block on
// the broker. The sync path establishes a fresh broker connection per call
// (matching the CLI pattern in cli/journal.go) so the server doesn't hold an
// IBKR ClientID slot when idle.
package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/server"
	"github.com/IS908/optix/pkg/model"
)

// JournalDeps is injected by the server bootstrap.
type JournalDeps struct {
	Service *server.JournalService
	Store   *sqlite.Store
}

func (s *Server) handleAPIJournalList(w http.ResponseWriter, r *http.Request) {
	if s.journal == nil {
		writeErrorJSON(w, "journal service unavailable", http.StatusServiceUnavailable)
		return
	}
	f := journalFilterFromQuery(r)
	execs, err := s.journal.Store.ListExecutions(r.Context(), f)
	if err != nil {
		writeErrorJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"executions": execs})
}

func (s *Server) handleAPIJournalTrips(w http.ResponseWriter, r *http.Request) {
	if s.journal == nil {
		writeErrorJSON(w, "journal service unavailable", http.StatusServiceUnavailable)
		return
	}
	rf := server.RoundTripFilter{
		Symbol: r.URL.Query().Get("symbol"),
		Status: r.URL.Query().Get("status"),
	}
	if v := r.URL.Query().Get("since"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			rf.Since = t
		}
	}
	if v := r.URL.Query().Get("until"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			rf.Until = t.Add(24*time.Hour - time.Second)
		}
	}
	trips, err := s.journal.Service.ListRoundTrips(r.Context(), rf)
	if err != nil {
		writeErrorJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"round_trips": trips})
}

func (s *Server) handleAPIJournalReview(w http.ResponseWriter, r *http.Request) {
	if s.journal == nil {
		writeErrorJSON(w, "journal service unavailable", http.StatusServiceUnavailable)
		return
	}
	var since, until time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			since = t
		}
	}
	if v := r.URL.Query().Get("until"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			until = t.Add(24*time.Hour - time.Second)
		}
	}
	sum, err := s.journal.Service.Review(r.Context(), since, until)
	if err != nil {
		writeErrorJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sum)
}

func (s *Server) handleAPIJournalSync(w http.ResponseWriter, r *http.Request) {
	if s.journal == nil {
		writeErrorJSON(w, "journal service unavailable", http.StatusServiceUnavailable)
		return
	}
	n, err := s.journal.Service.SyncExecutions(r.Context())
	st, _ := s.journal.Service.SyncState(r.Context())
	total, _ := s.journal.Store.ListExecutions(r.Context(), sqlite.JournalFilter{})
	payload := map[string]any{
		"new_count":    n,
		"total_count":  len(total),
		"last_sync_at": isoOrEmptyJ(st.LastSyncAt),
		"ibkr_ok":      err == nil,
		"error":        errStringJ(err),
	}
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func journalFilterFromQuery(r *http.Request) sqlite.JournalFilter {
	q := r.URL.Query()
	f := sqlite.JournalFilter{
		Symbol:  q.Get("symbol"),
		Side:    q.Get("side"),
		SecType: q.Get("type"),
	}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.Since = t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.Until = t.Add(24*time.Hour - time.Second)
		}
	}
	return f
}

func isoOrEmptyJ(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func errStringJ(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

// ─── /journal HTML page ───────────────────────────────────────────────────────

// journalPageData drives static/templates/journal.html.
// GeneratedAt + FromCache satisfy the shared footer block in base.html — if
// they were missing, the template engine would emit
// "can't evaluate field GeneratedAt" into the response body.
type journalPageData struct {
	GeneratedAt     time.Time
	FromCache       bool
	GapWarning      bool
	NeverSynced     bool // true → suppress "X hours ago" copy, show first-time hint
	HoursSinceSync  float64
	HumanGap        string // pretty-printed gap, e.g. "9.0 days" / "5.3 hours"
	TotalExecutions int
	Executions      []model.Execution
}

func (s *Server) handleJournalPage(w http.ResponseWriter, r *http.Request) {
	if s.journal == nil {
		writeErrorPage(w, "journal service unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()
	execs, err := s.journal.Store.ListExecutions(ctx, sqlite.JournalFilter{Limit: 200})
	if err != nil {
		writeErrorPage(w, err.Error(), http.StatusInternalServerError)
		return
	}
	st, _ := s.journal.Service.SyncState(ctx)
	var (
		hours       float64
		gap         = true
		neverSynced = st.LastSyncAt.IsZero()
		humanGap    string
	)
	if !neverSynced {
		hours = time.Since(st.LastSyncAt).Hours()
		gap = hours > server.GapWarningHours
		humanGap = formatGap(hours)
	}
	totalAll, _ := s.journal.Store.ListExecutions(ctx, sqlite.JournalFilter{})
	renderPage(w, "journal.html", journalPageData{
		GeneratedAt:     time.Now().UTC(),
		FromCache:       false, // /journal is always rendered from SQLite reads, but data freshness is conveyed via the gap-warning banner, not the footer's cache pill.
		GapWarning:      gap,
		NeverSynced:     neverSynced,
		HoursSinceSync:  hours,
		HumanGap:        humanGap,
		TotalExecutions: len(totalAll),
		Executions:      execs,
	})
}

// formatGap returns a human-friendly description of "time since last sync".
// Switches to days when the gap exceeds 48 hours so week-scale gaps don't
// render as "144.0 hours ago".
func formatGap(hours float64) string {
	if hours >= 48 {
		return fmt.Sprintf("%.1f days", hours/24)
	}
	return fmt.Sprintf("%.1f hours", hours)
}

