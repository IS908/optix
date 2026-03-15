package webui

import (
	"net/http"
	"strings"
)

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
