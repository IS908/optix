package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
	"github.com/IS908/optix/internal/analysis"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/server"
	"github.com/IS908/optix/internal/watchlist"
	"github.com/IS908/optix/pkg/model"
)

// newIBClient creates a new IB client with the configured host/port.
// clientID 4 is reserved for web UI single-symbol analyze;
// clientID 5 is reserved for web UI dashboard live refresh.
func (s *Server) newIBClient(clientID int) *ibkr.Client {
	return ibkr.New(ibkr.Config{
		Host:     s.cfg.IBHost,
		Port:     s.cfg.IBPort,
		ClientID: int64(clientID),
	})
}

// fetchLiveAnalysis runs the full IB + Python pipeline for one symbol.
func (s *Server) fetchLiveAnalysis(ctx context.Context, symbol string) (*AnalyzeResponse, error) {
	ibClient := s.newIBClient(4)
	if err := ibClient.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect to IB TWS: %w", err)
	}
	defer ibClient.Disconnect()

	svc := server.NewMarketDataService(ibClient, s.store)

	stockData, err := server.FetchSymbolData(ctx, symbol, svc, ibClient)
	if err != nil {
		return nil, fmt.Errorf("fetch market data: %w", err)
	}

	analysisClient, err := analysis.NewClient(s.cfg.AnalysisAddr)
	if err != nil {
		return nil, fmt.Errorf("connect analysis engine: %w", err)
	}
	defer analysisClient.Close()

	analyzeCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	protoResp, err := analysisClient.AnalyzeStock(analyzeCtx, &analysisv1.AnalyzeStockRequest{
		Symbol:           symbol,
		ForecastDays:     s.cfg.ForecastDays,
		AvailableCapital: s.cfg.Capital,
		RiskTolerance:    s.cfg.RiskTolerance,
		HistoricalBars:   stockData.HistoricalBars,
		OptionChain:      stockData.OptionChain,
		CurrentQuote:     stockData.Quote,
	})
	if err != nil {
		return nil, fmt.Errorf("analysis engine: %w", err)
	}

	resp := ProtoToAnalyzeResponse(protoResp, symbol, true)

	// Persist to cache for future cache-mode hits
	if payload, jerr := json.Marshal(resp); jerr == nil {
		_ = s.store.SaveAnalysisCache(ctx, symbol, payload)
	}

	// Save a daily watchlist snapshot so the dashboard cache stays up-to-date.
	snap := model.QuickSummary{
		Symbol:      symbol,
		Price:       resp.Summary.Price,
		Trend:       resp.Technical.Trend,
		RSI:         resp.Technical.RSI14,
		IVRank:      resp.Options.IVRank,
		MaxPain:     resp.Options.MaxPain,
		PCR:         resp.Options.PCROi,
		RangeLow1S:  resp.Outlook.RangeLow1S,
		RangeHigh1S: resp.Outlook.RangeHigh1S,
	}
	if len(resp.Strategies) > 0 {
		snap.Recommendation   = resp.Strategies[0].StrategyName
		snap.OpportunityScore = resp.Strategies[0].Score
	}
	_ = s.store.SaveWatchlistSnapshot(ctx, snap)

	// For a live fetch every layer was just refreshed — use current time.
	now := time.Now().UTC()
	resp.Freshness.Symbol     = symbol
	resp.Freshness.QuoteAt    = now
	resp.Freshness.OHLCVAt    = now
	resp.Freshness.OptionsAt  = now
	resp.Freshness.CacheAt    = now
	resp.Freshness.SnapshotAt = now

	return resp, nil
}

// fetchLiveDashboard fetches all watchlist symbols concurrently (max 5 at a time)
// and runs batch quick analysis on the Python engine.
// Overall timeout: 3 minutes for the entire dashboard refresh.
func (s *Server) fetchLiveDashboard(ctx context.Context) (*DashboardResponse, error) {
	// Set overall timeout for the entire operation
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	mgr := watchlist.NewManager(s.store)
	items, err := mgr.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("get watchlist: %w", err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("watchlist is empty — add symbols with 'optix watch add AAPL'")
	}

	ibClient := s.newIBClient(5)
	if err := ibClient.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect to IB TWS: %w", err)
	}
	defer ibClient.Disconnect()

	svc := server.NewMarketDataService(ibClient, s.store)

	// Bounded-concurrency fetch (max 5 simultaneous IB requests for faster processing)
	type result struct {
		idx  int
		data *analysisv1.SingleStockData
		err  error
	}
	results := make(chan result, len(items))
	sem := make(chan struct{}, 5) // Increased from 2 to 5

	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func(idx int, sym string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d, e := server.FetchSymbolData(ctx, sym, svc, ibClient)
			results <- result{idx: idx, data: d, err: e}
		}(i, item.Symbol)
	}
	go func() { wg.Wait(); close(results) }()

	ordered := make([]*analysisv1.SingleStockData, len(items))
	for r := range results {
		if r.err == nil && r.data != nil {
			ordered[r.idx] = r.data
		}
	}

	// Collect successfully fetched symbols
	var stocks []*analysisv1.SingleStockData
	for _, d := range ordered {
		if d != nil {
			stocks = append(stocks, d)
		}
	}
	if len(stocks) == 0 {
		return nil, fmt.Errorf("failed to fetch market data for all symbols")
	}

	// Run batch quick analysis on Python engine
	analysisClient, err := analysis.NewClient(s.cfg.AnalysisAddr)
	if err != nil {
		return nil, fmt.Errorf("connect analysis engine: %w", err)
	}
	defer analysisClient.Close()

	// Use remaining time from parent context, or 90 seconds (whichever is shorter)
	batchCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	batchResp, err := analysisClient.BatchQuickAnalysis(batchCtx, &analysisv1.BatchQuickAnalysisRequest{
		Stocks:           stocks,
		ForecastDays:     s.cfg.ForecastDays,
		AvailableCapital: s.cfg.Capital,
	})
	if err != nil {
		return nil, fmt.Errorf("batch analysis (fetched %d/%d symbols): %w", len(stocks), len(items), err)
	}

	now := time.Now().UTC()

	syms := make([]SymbolSummary, 0, len(batchResp.Summaries))
	for _, sm := range batchResp.Summaries {
		syms = append(syms, SymbolSummary{
			Symbol:           sm.Symbol,
			Price:            sm.Price,
			Trend:            sm.Trend,
			RSI:              sm.Rsi,
			IVRank:           sm.IvRank,
			MaxPain:          sm.MaxPain,
			PCR:              sm.Pcr,
			RangeLow1S:       sm.RangeLow_1S,
			RangeHigh1S:      sm.RangeHigh_1S,
			Recommendation:   sm.Recommendation,
			OpportunityScore: sm.OpportunityScore,
			SnapshotDate:     "live",
		})
		// Persist as a daily snapshot so the dashboard cache path stays fresh.
		_ = s.store.SaveWatchlistSnapshot(ctx, model.QuickSummary{
			Symbol:           sm.Symbol,
			Price:            sm.Price,
			Trend:            sm.Trend,
			RSI:              sm.Rsi,
			IVRank:           sm.IvRank,
			MaxPain:          sm.MaxPain,
			PCR:              sm.Pcr,
			RangeLow1S:       sm.RangeLow_1S,
			RangeHigh1S:      sm.RangeHigh_1S,
			Recommendation:   sm.Recommendation,
			OpportunityScore: sm.OpportunityScore,
		})
	}

	// Build freshness for each successfully fetched symbol (all layers = now,
	// including SnapshotAt since we just saved the snapshot above).
	freshness := make([]model.SymbolFreshness, 0, len(syms))
	for _, sym := range syms {
		freshness = append(freshness, model.SymbolFreshness{
			Symbol:     sym.Symbol,
			QuoteAt:    now,
			OHLCVAt:    now,
			OptionsAt:  now,
			SnapshotAt: now,
			// CacheAt is not written during a live dashboard refresh
		})
	}

	// Backfill CacheAt from DB — live dashboard runs batch quick analysis (not
	// full analysis), so CacheAt reflects a previous fetchLiveAnalysis run.
	if dbFreshAll, dbErr := s.store.GetAllSymbolFreshness(ctx); dbErr == nil {
		cacheAtMap := make(map[string]time.Time, len(dbFreshAll))
		for _, f := range dbFreshAll {
			cacheAtMap[f.Symbol] = f.CacheAt
		}
		for i := range freshness {
			freshness[i].CacheAt = cacheAtMap[freshness[i].Symbol]
		}
	}

	return &DashboardResponse{
		GeneratedAt: time.Now().UTC(),
		FromCache:   false,
		Symbols:     syms,
		Freshness:   freshness,
	}, nil
}
