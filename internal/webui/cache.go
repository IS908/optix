package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// fetchCachedDashboard loads the latest watchlist snapshot from SQLite.
func (s *Server) fetchCachedDashboard(ctx context.Context) (*DashboardResponse, error) {
	snaps, err := s.store.GetLatestSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("load snapshots: %w", err)
	}

	// Fetch freshness data for all watchlist symbols regardless of snapshot presence.
	freshAll, _ := s.store.GetAllSymbolFreshness(ctx) // best-effort; ignore error

	if len(snaps) == 0 {
		// No snapshots yet — return an empty response so the template renders
		// the "no data" empty state instead of a 500 error page.
		return &DashboardResponse{
			GeneratedAt: time.Now().UTC(),
			FromCache:   true,
			Freshness:   freshAll,
		}, nil
	}

	syms := make([]SymbolSummary, 0, len(snaps))
	for _, snap := range snaps {
		syms = append(syms, snapToSymbolSummary(snap))
	}
	return &DashboardResponse{
		GeneratedAt: time.Now().UTC(),
		FromCache:   true,
		Symbols:     syms,
		Freshness:   freshAll,
	}, nil
}

// fetchCachedAnalysis retrieves a previously saved full analysis from SQLite.
func (s *Server) fetchCachedAnalysis(ctx context.Context, symbol string) (*AnalyzeResponse, error) {
	payload, cachedAt, err := s.store.GetAnalysisCache(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("no cached analysis for %s", symbol)
	}
	var resp AnalyzeResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("corrupt cache for %s: %w", symbol, err)
	}
	resp.FromCache = true
	resp.GeneratedAt = cachedAt

	// Populate per-data-layer freshness from SQLite.
	resp.Freshness, _ = s.store.GetSymbolFreshness(ctx, symbol) // best-effort

	return &resp, nil
}
