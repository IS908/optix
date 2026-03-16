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
	// Clean up derived caches so the removed symbol no longer appears in the
	// dashboard or analyze page (best-effort — don't fail the remove if these error).
	_ = s.store.DeleteWatchlistSnapshots(r.Context(), symbol)
	_ = s.store.DeleteAnalysisCache(r.Context(), symbol)

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
	var resp *DashboardResponse
	var finalErr error

	if r.URL.Query().Get("refresh") == "true" {
		live, liveErr := s.fetchLiveDashboard(r.Context())
		if liveErr != nil {
			// Live fetch failed — try to serve cached data with a warning banner.
			if cached, cacheErr := s.fetchCachedDashboard(r.Context()); cacheErr == nil {
				resp = cached
				resp.Error = liveErr.Error()
			} else {
				finalErr = liveErr
			}
		} else {
			resp = live
		}
	} else {
		var err error
		resp, err = s.fetchCachedDashboard(r.Context())
		if err != nil {
			finalErr = err
		}
	}

	if finalErr != nil {
		writeErrorPage(w, finalErr.Error(), http.StatusInternalServerError)
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
	return s.fetchCachedDashboard(r.Context())
}

// ─── Analyze ─────────────────────────────────────────────────────────────────

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(r.PathValue("symbol"))
	if symbol == "" {
		writeErrorPage(w, "missing symbol", http.StatusBadRequest)
		return
	}

	var resp *AnalyzeResponse
	var finalErr error

	if r.URL.Query().Get("refresh") == "true" {
		live, liveErr := s.fetchLiveAnalysis(r.Context(), symbol)
		if liveErr != nil {
			// Live fetch failed — try to serve cached data with a warning banner.
			if cached, cacheErr := s.fetchCachedAnalysis(r.Context(), symbol); cacheErr == nil {
				resp = cached
				resp.Error = liveErr.Error()
			} else {
				finalErr = liveErr
			}
		} else {
			resp = live
		}
	} else {
		var err error
		resp, err = s.fetchCachedAnalysis(r.Context(), symbol)
		if err != nil {
			finalErr = err
		}
	}

	if finalErr != nil {
		// No data anywhere — render a friendly empty-state page with the error.
		freshness, _ := s.store.GetSymbolFreshness(r.Context(), symbol)
		// Still trigger a background refresh so the next page load may have data.
		s.maybeBackgroundRefresh(symbol)
		renderPage(w, "analyze.html", &AnalyzeResponse{
			Symbol:    symbol,
			NoData:    true,
			Error:     finalErr.Error(),
			Freshness: freshness,
		})
		return
	}
	// Trigger a background refresh to keep the cache warm (rate-limited to 3 min).
	s.maybeBackgroundRefresh(symbol)
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
	return s.fetchCachedAnalysis(r.Context(), symbol)
}
