package webui

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/IS908/optix/internal/watchlist"
)

// ─── Watchlist ────────────────────────────────────────────────────────────────

func (s *Server) handleWatchlist(w http.ResponseWriter, r *http.Request) {
	wm := watchlist.NewManager(s.store)
	items, err := wm.List(r.Context())
	if err != nil {
		writeErrorPage(w, "failed to load watchlist: "+err.Error(), http.StatusInternalServerError)
		return
	}
	renderPage(w, "watchlist.html", &WatchlistPageResponse{
		GeneratedAt:  time.Now().UTC(),
		Items:        items,
		FlashError:   r.URL.Query().Get("error"),
		FlashSuccess: r.URL.Query().Get("success"),
	})
}

func (s *Server) handleWatchlistAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/watchlist?error="+url.QueryEscape("invalid form"), http.StatusSeeOther)
		return
	}
	raw := strings.TrimSpace(r.FormValue("symbols"))
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		http.Redirect(w, r, "/watchlist?error="+url.QueryEscape("please enter at least one symbol"), http.StatusSeeOther)
		return
	}
	wm := watchlist.NewManager(s.store)
	if err := wm.Add(r.Context(), parts...); err != nil {
		http.Redirect(w, r, "/watchlist?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	added := strings.Join(parts, ", ")
	http.Redirect(w, r, "/watchlist?success="+url.QueryEscape("Added: "+added), http.StatusSeeOther)
}

func (s *Server) handleWatchlistRemove(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(strings.TrimSpace(r.PathValue("symbol")))
	if symbol == "" {
		http.Redirect(w, r, "/watchlist?error="+url.QueryEscape("missing symbol"), http.StatusSeeOther)
		return
	}
	wm := watchlist.NewManager(s.store)
	if err := wm.Remove(r.Context(), symbol); err != nil {
		http.Redirect(w, r, "/watchlist?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/watchlist?success="+url.QueryEscape("Removed: "+symbol), http.StatusSeeOther)
}

// ─── Help ─────────────────────────────────────────────────────────────────────

func (s *Server) handleHelp(w http.ResponseWriter, r *http.Request) {
	renderPage(w, "help.html", map[string]any{
		"GeneratedAt": time.Now().UTC(),
	})
}

// ─── Dashboard ────────────────────────────────────────────────────────────────

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	resp, err := s.getDashboardData(r)
	if err != nil {
		writeErrorPage(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPage(w, "dashboard.html", resp)
}

func (s *Server) handleAPIDashboard(w http.ResponseWriter, r *http.Request) {
	resp, err := s.getDashboardData(r)
	if err != nil {
		writeErrorJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, resp)
}

func (s *Server) getDashboardData(r *http.Request) (*DashboardResponse, error) {
	if r.URL.Query().Get("refresh") == "true" {
		return s.fetchLiveDashboard(r.Context())
	}
	resp, err := s.fetchCachedDashboard(r.Context())
	if err != nil {
		// Fall-back hint embedded in the error
		return nil, err
	}
	return resp, nil
}

// ─── Analyze ─────────────────────────────────────────────────────────────────

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(r.PathValue("symbol"))
	if symbol == "" {
		writeErrorPage(w, "missing symbol", http.StatusBadRequest)
		return
	}

	resp, err := s.getAnalyzeData(r, symbol)
	if err != nil {
		writeErrorPage(w, err.Error(), http.StatusNotFound)
		return
	}
	renderPage(w, "analyze.html", resp)
}

func (s *Server) handleAPIAnalyze(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(r.PathValue("symbol"))
	if symbol == "" {
		writeErrorJSON(w, "missing symbol", http.StatusBadRequest)
		return
	}

	resp, err := s.getAnalyzeData(r, symbol)
	if err != nil {
		writeErrorJSON(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, resp)
}

func (s *Server) getAnalyzeData(r *http.Request, symbol string) (*AnalyzeResponse, error) {
	if r.URL.Query().Get("refresh") == "true" {
		return s.fetchLiveAnalysis(r.Context(), symbol)
	}
	resp, err := s.fetchCachedAnalysis(r.Context(), symbol)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
