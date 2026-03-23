package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/IS908/optix/internal/analysis"
	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/server"
	"github.com/IS908/optix/internal/watchlist"
	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
	"github.com/IS908/optix/pkg/model"
	"github.com/spf13/cobra"
)

func newDashboardCmd() *cobra.Command {
	var capital float64
	var sortBy string
	var top int
	var analysisAddr string

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Show watchlist overview with key indicators and recommendations",
		Long: `Scan all watchlist stocks and display a quick summary table:
symbol, price, trend, RSI, IV Rank, Max Pain, PCR, 2-week price range, recommendation.

Examples:
  optix dashboard
  optix dashboard --sort=iv-rank --top=5
  optix dashboard --capital=100000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// 1. Open SQLite store
			store, err := sqlite.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			RegisterCleanup(store)
			defer store.Close()

			// 2. Read watchlist
			mgr := watchlist.NewManager(store)
			items, err := mgr.List(ctx)
			if err != nil {
				return fmt.Errorf("get watchlist: %w", err)
			}
			if len(items) == 0 {
				fmt.Println("Watchlist is empty. Use 'optix watch add AAPL TSLA' to add symbols.")
				return nil
			}

			fmt.Printf("📋 Dashboard — %d symbols\n", len(items))
			fmt.Printf("⏳ Connecting to market data source...\n")

			// 3. Connect to broker (IBKR with yfinance fallback)
			b := broker.NewWithFallback(ibkr.Config{
				Host:     ibHost,
				Port:     ibPort,
				ClientID: 3,
			}, pythonBin)
			if err := b.Connect(ctx); err != nil {
				return fmt.Errorf("connect to broker: %w", err)
			}
			defer b.Disconnect()
			fmt.Println(b.SourceBanner())

			svc := server.NewMarketDataService(b, store)

			// 4. Fetch data for each symbol (bounded concurrency = 2)
			fmt.Printf("📊 Fetching market data...\n")

			type fetchResult struct {
				idx  int
				data *analysisv1.SingleStockData
				err  error
			}

			resultsCh := make(chan fetchResult, len(items))
			sem := make(chan struct{}, 2) // max 2 concurrent IB requests

			var wg sync.WaitGroup
			for i, item := range items {
				wg.Add(1)
				go func(idx int, sym string) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					data, fetchErr := fetchSymbolData(ctx, sym, svc)
					resultsCh <- fetchResult{idx: idx, data: data, err: fetchErr}
					if fetchErr != nil {
						fmt.Printf("  ⚠️  %-6s  %v\n", sym, fetchErr)
					} else {
						fmt.Printf("  ✓  %s\n", sym)
					}
				}(i, item.Symbol)
			}

			// Close channel after all goroutines finish
			go func() {
				wg.Wait()
				close(resultsCh)
			}()

			// Collect results maintaining original order
			orderedData := make([]*analysisv1.SingleStockData, len(items))
			for r := range resultsCh {
				if r.err == nil && r.data != nil {
					orderedData[r.idx] = r.data
				}
			}

			// Build stocks slice (skip failed fetches)
			var stocks []*analysisv1.SingleStockData
			for _, d := range orderedData {
				if d != nil {
					stocks = append(stocks, d)
				}
			}
			if len(stocks) == 0 {
				return fmt.Errorf("failed to fetch data for all watchlist symbols")
			}

			// 5. Connect to Python analysis engine
			fmt.Printf("🔬 Running batch analysis on %d symbols via %s...\n", len(stocks), analysisAddr)
			analysisClient, err := analysis.NewClient(analysisAddr)
			if err != nil {
				return fmt.Errorf("connect to analysis engine: %w", err)
			}
			defer analysisClient.Close()

			batchCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
			defer cancel()

			batchResp, err := analysisClient.BatchQuickAnalysis(batchCtx, &analysisv1.BatchQuickAnalysisRequest{
				Stocks:           stocks,
				ForecastDays:     14,
				AvailableCapital: capital,
			})
			if err != nil {
				return fmt.Errorf("batch analysis: %w", err)
			}

			summaries := batchResp.Summaries

			// 6. Sort
			summaries = dashboardSort(summaries, sortBy)

			// 7. Apply --top limit
			if top > 0 && top < len(summaries) {
				summaries = summaries[:top]
			}

			// 8. Save snapshots to SQLite (best-effort)
			for _, s := range summaries {
				snap := model.QuickSummary{
					Symbol:           s.Symbol,
					Price:            s.Price,
					Trend:            s.Trend,
					RSI:              s.Rsi,
					IVRank:           s.IvRank,
					MaxPain:          s.MaxPain,
					PCR:              s.Pcr,
					RangeLow1S:       s.RangeLow_1S,
					RangeHigh1S:      s.RangeHigh_1S,
					Recommendation:   s.Recommendation,
					OpportunityScore: s.OpportunityScore,
				}
				if saveErr := store.SaveWatchlistSnapshot(ctx, snap); saveErr != nil {
					// Non-fatal
					_ = saveErr
				}
			}

			// 9. Print table
			printDashboard(summaries, sortBy, b.SourceName())
			return nil
		},
	}

	cmd.Flags().Float64Var(&capital, "capital", 100000, "Available capital for strategy sizing")
	cmd.Flags().StringVar(&sortBy, "sort", "opportunity", "Sort by: opportunity, iv-rank, trend, pcr")
	cmd.Flags().IntVar(&top, "top", 0, "Show only top N results (0 = all)")
	cmd.Flags().StringVar(&analysisAddr, "analysis-addr", "localhost:50052", "Python analysis engine gRPC address")

	return cmd
}

// dashboardSort sorts quick summaries by the given key.
func dashboardSort(summaries []*analysisv1.StockQuickSummary, by string) []*analysisv1.StockQuickSummary {
	sort.SliceStable(summaries, func(i, j int) bool {
		a, b := summaries[i], summaries[j]
		switch by {
		case "iv-rank":
			return a.IvRank > b.IvRank
		case "trend":
			return dashTrendScore(a.Trend) > dashTrendScore(b.Trend)
		case "pcr":
			// Most extreme PCR (furthest from 1.0) first
			da := a.Pcr - 1.0
			if da < 0 {
				da = -da
			}
			db := b.Pcr - 1.0
			if db < 0 {
				db = -db
			}
			return da > db
		default: // "opportunity"
			return a.OpportunityScore > b.OpportunityScore
		}
	})
	return summaries
}

func dashTrendScore(trend string) int {
	switch strings.ToLower(trend) {
	case "bullish":
		return 2
	case "neutral":
		return 1
	default:
		return 0
	}
}

// ─── Dashboard table printing ─────────────────────────────────────────────────

// Column widths (not counting the border chars / padding spaces).
const (
	dashW_sym  = 8
	dashW_prc  = 8
	dashW_trd  = 8
	dashW_rsi  = 5
	dashW_iv   = 8
	dashW_mp   = 9
	dashW_pcr  = 6
	dashW_rng  = 14
	dashW_rec  = 20
)

func dashBorder(left, mid, right, fill string) string {
	cols := []int{dashW_sym, dashW_prc, dashW_trd, dashW_rsi, dashW_iv, dashW_mp, dashW_pcr, dashW_rng, dashW_rec}
	var sb strings.Builder
	sb.WriteString(left)
	for i, w := range cols {
		sb.WriteString(strings.Repeat(fill, w+2))
		if i < len(cols)-1 {
			sb.WriteString(mid)
		}
	}
	sb.WriteString(right)
	return sb.String()
}

func dashRow(cells [9]string) string {
	widths := [9]int{dashW_sym, dashW_prc, dashW_trd, dashW_rsi, dashW_iv, dashW_mp, dashW_pcr, dashW_rng, dashW_rec}
	var sb strings.Builder
	sb.WriteString("║")
	for i, c := range cells {
		sb.WriteString(fmt.Sprintf(" %-*s ", widths[i], dashTruncate(c, widths[i])))
		if i < len(cells)-1 {
			sb.WriteString("│")
		}
	}
	sb.WriteString("║")
	return sb.String()
}

func dashTruncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func dashTrendArrow(trend string) string {
	switch strings.ToLower(trend) {
	case "bullish":
		return "↑ Bull"
	case "bearish":
		return "↓ Bear"
	default:
		return "→ Neut"
	}
}

func printDashboard(summaries []*analysisv1.StockQuickSummary, sortBy, dataSource string) {
	if len(summaries) == 0 {
		fmt.Println("No results to display.")
		return
	}

	top := dashBorder("╔", "╤", "╗", "═")
	sep := dashBorder("╠", "╪", "╣", "═")
	bot := dashBorder("╚", "╧", "╝", "═")

	header := dashRow([9]string{"Symbol", "Price", "Trend", "RSI", "IV Rank", "Max Pain", "PCR", "2W Range (1σ)", "Recommendation"})

	fmt.Println()
	fmt.Println(top)
	fmt.Println(header)
	fmt.Println(sep)

	for _, s := range summaries {
		sym := s.Symbol
		// Star prefix for high-opportunity stocks
		if s.OpportunityScore >= 50 {
			sym = "★" + sym
		}

		price := fmt.Sprintf("$%.2f", s.Price)
		trend := dashTrendArrow(s.Trend)
		rsi := fmt.Sprintf("%.0f", s.Rsi)
		iv := fmt.Sprintf("%.0f%%", s.IvRank)

		var mp string
		if s.MaxPain > 0 {
			mp = fmt.Sprintf("$%.0f", s.MaxPain)
		} else {
			mp = "N/A"
		}

		pcr := fmt.Sprintf("%.2f", s.Pcr)

		var rng string
		if s.RangeLow_1S > 0 && s.RangeHigh_1S > 0 {
			rng = fmt.Sprintf("$%.0f-$%.0f", s.RangeLow_1S, s.RangeHigh_1S)
		} else {
			rng = "N/A"
		}

		rec := s.Recommendation

		fmt.Println(dashRow([9]string{sym, price, trend, rsi, iv, mp, pcr, rng, rec}))
	}

	fmt.Println(bot)
	fmt.Printf("\n★ = Opportunity score ≥ 50  │  Sort: %s  │  Source: %s  │  %s\n\n",
		sortBy, dataSource, time.Now().Format("2006-01-02 15:04:05"))
}
