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
	if len(snaps) == 0 {
		return nil, fmt.Errorf("no snapshot data — run 'optix dashboard' or use ?refresh=true")
	}

	syms := make([]SymbolSummary, 0, len(snaps))
	for _, snap := range snaps {
		syms = append(syms, snapToSymbolSummary(snap))
	}
	return &DashboardResponse{
		GeneratedAt: time.Now().UTC(),
		FromCache:   true,
		Symbols:     syms,
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
	return &resp, nil
}
