package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
	"github.com/IS908/optix/internal/analysis"
	"github.com/IS908/optix/internal/server"
	"github.com/IS908/optix/internal/watchlist"
	"github.com/IS908/optix/pkg/model"
)

// fetchLiveAnalysis deduplicates concurrent requests for the same symbol via
// singleflight: only one live fetch runs at a time per symbol.
func (s *Server) fetchLiveAnalysis(ctx context.Context, symbol string) (*AnalyzeResponse, error) {
	v, err, _ := s.sfGroup.Do("analyze:"+symbol, func() (any, error) {
		return s.doFetchLiveAnalysis(ctx, symbol)
	})
	if err != nil {
		return nil, err
	}
	return v.(*AnalyzeResponse), nil
}

// doFetchLiveAnalysis runs the full broker + Python pipeline for one symbol.
// It acquires a slot from the broker pool (blocking if all slots are busy),
// uses the pool's persistent connection, and releases the slot on return.
// The slot is marked unhealthy if the broker drops mid-request, triggering
// an async reconnect before the slot re-enters the pool.
func (s *Server) doFetchLiveAnalysis(ctx context.Context, symbol string) (*AnalyzeResponse, error) {
	conn, err := s.brokerPool.acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect to broker: %w", err)
	}
	healthy := true
	defer func() { s.brokerPool.release(conn, healthy) }()

	svc := server.NewMarketDataService(conn.b, s.store)

	stockData, err := server.FetchSymbolData(ctx, symbol, svc)
	if err != nil {
		healthy = conn.isConnected()
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
	resp.DataSource = conn.sourceName()
	session := model.USMarketSession(time.Now())
	resp.MarketSession = string(session)
	resp.SessionLabel = session.Label()

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

	// Read freshness from SQLite so it reflects actual per-layer timestamps
	// (e.g. quote trade time, bar open_time) rather than a blanket time.Now().
	// This also avoids a visual "jump" when the frontend polls /api/freshness
	// 30s later — both the initial render and the poll read from the same source.
	resp.Freshness, _ = s.store.GetSymbolFreshness(ctx, symbol)

	return resp, nil
}

// fetchLiveDashboard deduplicates concurrent dashboard refresh requests via
// singleflight: only one live fetch runs at a time.
func (s *Server) fetchLiveDashboard(ctx context.Context) (*DashboardResponse, error) {
	v, err, _ := s.sfGroup.Do("dashboard", func() (any, error) {
		return s.doFetchLiveDashboard(ctx)
	})
	if err != nil {
		return nil, err
	}
	return v.(*DashboardResponse), nil
}

// doFetchLiveDashboard fetches all watchlist symbols concurrently (max 5 at a time)
// and runs batch quick analysis on the Python engine.
// Overall timeout: 3 minutes for the entire dashboard refresh.
func (s *Server) doFetchLiveDashboard(ctx context.Context) (*DashboardResponse, error) {
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

	conn, err := s.brokerPool.acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect to broker: %w", err)
	}
	healthy := true
	defer func() { s.brokerPool.release(conn, healthy) }()

	svc := server.NewMarketDataService(conn.b, s.store)

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
			// Per-symbol timeout so invalid symbols don't block the pool
			symCtx, symCancel := context.WithTimeout(ctx, 30*time.Second)
			defer symCancel()
			d, e := server.FetchSymbolData(symCtx, sym, svc)
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

	// Read freshness from SQLite so each column reflects its actual timestamp
	// (quote trade time, bar open_time, option snapshot_time, etc.) rather
	// than a blanket time.Now(). This also keeps the initial render consistent
	// with subsequent /api/freshness polls, avoiding visual "jumps".
	freshness, _ := s.store.GetAllSymbolFreshness(ctx) // best-effort

	session := model.USMarketSession(time.Now())
	return &DashboardResponse{
		GeneratedAt:   time.Now().UTC(),
		FromCache:     false,
		DataSource:    conn.sourceName(),
		Symbols:       syms,
		Freshness:     freshness,
		MarketSession: string(session),
		SessionLabel:  session.Label(),
	}, nil
}
