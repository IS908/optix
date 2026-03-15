// Package webui implements the Optix lightweight web UI server.
// It serves HTML pages and a JSON API backed by a SQLite cache (default) or
// live IB TWS + Python analysis engine calls (?refresh=true).
package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
)

// Config holds all configuration for the web UI server.
type Config struct {
	Addr         string  // HTTP listen address, e.g. "0.0.0.0:8080"
	IBHost       string
	IBPort       int
	AnalysisAddr string
	Capital      float64
	ForecastDays int32
	RiskTolerance string
}

// Server is the Optix web UI HTTP server.
type Server struct {
	cfg   Config
	store *sqlite.Store
	mux   *http.ServeMux
}

// New creates a Server and registers all routes.
func New(cfg Config, store *sqlite.Store) *Server {
	if cfg.ForecastDays == 0 {
		cfg.ForecastDays = 14
	}
	if cfg.RiskTolerance == "" {
		cfg.RiskTolerance = "moderate"
	}

	s := &Server{cfg: cfg, store: store}
	s.mux = http.NewServeMux()
	s.registerRoutes()
	return s
}

// Start begins serving HTTP requests. It blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:         s.cfg.Addr,
		Handler:      s.mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // long for live fetches
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("Optix web UI  →  http://%s\n", s.cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

// registerRoutes wires all HTTP routes using Go 1.22+ pattern matching.
func (s *Server) registerRoutes() {
	// Static assets (CSS, etc.)
	s.mux.Handle("GET /static/",
		http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// HTML pages
	s.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	})
	s.mux.HandleFunc("GET /dashboard", s.handleDashboard)
	s.mux.HandleFunc("GET /analyze/{symbol}", s.handleAnalyze)
	s.mux.HandleFunc("GET /help", s.handleHelp)

	// Watchlist management
	s.mux.HandleFunc("GET /watchlist", s.handleWatchlist)
	s.mux.HandleFunc("POST /watchlist", s.handleWatchlistAdd)
	s.mux.HandleFunc("POST /watchlist/{symbol}/remove", s.handleWatchlistRemove)

	// JSON API
	s.mux.HandleFunc("GET /api/dashboard", s.handleAPIDashboard)
	s.mux.HandleFunc("GET /api/analyze/{symbol}", s.handleAPIAnalyze)
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

func renderPage(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl, ok := pageTemplates[name]
	if !ok {
		http.Error(w, "unknown template: "+name, http.StatusInternalServerError)
		return
	}
	// Execute "base" — the entry point defined in base.html. Each page's
	// {{define "content"}} is isolated in its own template set, so there is
	// no cross-page override.
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeErrorJSON(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeErrorPage(w http.ResponseWriter, msg string, code int) {
	w.WriteHeader(code)
	renderPage(w, "error.html", map[string]any{"Error": msg, "Code": code})
}
