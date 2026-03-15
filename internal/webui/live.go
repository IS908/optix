package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/IS908/optix/internal/analysis"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/server"
	"github.com/IS908/optix/internal/watchlist"
	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
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

	resp := protoToAnalyzeResponse(protoResp, symbol, true)

	// Persist to cache for future cache-mode hits
	if payload, jerr := json.Marshal(resp); jerr == nil {
		_ = s.store.SaveAnalysisCache(ctx, symbol, payload)
	}

	return resp, nil
}

// fetchLiveDashboard fetches all watchlist symbols concurrently (max 2 at a time
// to respect IB pacing rules) and runs batch quick analysis on the Python engine.
func (s *Server) fetchLiveDashboard(ctx context.Context) (*DashboardResponse, error) {
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

	// Bounded-concurrency fetch (max 2 simultaneous IB requests)
	type result struct {
		idx  int
		data *analysisv1.SingleStockData
		err  error
	}
	results := make(chan result, len(items))
	sem := make(chan struct{}, 2)

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

	batchCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	batchResp, err := analysisClient.BatchQuickAnalysis(batchCtx, &analysisv1.BatchQuickAnalysisRequest{
		Stocks:           stocks,
		ForecastDays:     s.cfg.ForecastDays,
		AvailableCapital: s.cfg.Capital,
	})
	if err != nil {
		return nil, fmt.Errorf("batch analysis: %w", err)
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
	}

	return &DashboardResponse{
		GeneratedAt: time.Now().UTC(),
		FromCache:   false,
		Symbols:     syms,
	}, nil
}
