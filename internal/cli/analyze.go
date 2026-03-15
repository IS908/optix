package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/IS908/optix/internal/analysis"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/server"
	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
	marketdatav1 "github.com/IS908/optix/gen/go/optix/marketdata/v1"
	"github.com/spf13/cobra"
)

func newAnalyzeCmd() *cobra.Command {
	var weeks int
	var capital float64
	var risk string
	var useWatchlist bool
	var analysisAddr string

	cmd := &cobra.Command{
		Use:   "analyze [symbol]",
		Short: "Run full stock analysis with options strategy recommendations",
		Long: `Analyze a stock comprehensively: technical analysis, options data (OI, IV, Max Pain),
price range forecast, and sell-side options strategy recommendations.

Examples:
  optix analyze AAPL
  optix analyze AAPL --weeks=2 --capital=50000
  optix analyze AAPL --risk=conservative
  optix analyze --watchlist --capital=100000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			forecastDays := int32(weeks * 7)

			if useWatchlist {
				return runWatchlistAnalysis(ctx, forecastDays, capital, risk, analysisAddr)
			}

			if len(args) == 0 {
				return fmt.Errorf("please specify a symbol or use --watchlist")
			}

			symbol := strings.ToUpper(args[0])

			fmt.Printf("⏳ Connecting to IB TWS at %s:%d...\n", ibHost, ibPort)

			// Open SQLite store for caching
			store, err := sqlite.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer store.Close()

			// Connect to IB TWS
			ibClient := ibkr.New(ibkr.Config{
				Host:     ibHost,
				Port:     ibPort,
				ClientID: 2, // use client ID 2 for analyze (avoids conflict with quote cmd)
			})
			if err := ibClient.Connect(ctx); err != nil {
				return fmt.Errorf("connect to IB: %w", err)
			}
			defer ibClient.Disconnect()

			// Create MarketDataService with SQLite caching
			svc := server.NewMarketDataService(ibClient, store)

			// Fetch all data for this symbol
			fmt.Printf("📊 Fetching data for %s...\n", symbol)
			stockData, err := fetchSymbolData(ctx, symbol, svc, ibClient)
			if err != nil {
				return fmt.Errorf("fetch data: %w", err)
			}

			// Connect to Python analysis engine
			fmt.Printf("🔬 Running analysis engine at %s...\n", analysisAddr)
			analysisClient, err := analysis.NewClient(analysisAddr)
			if err != nil {
				return fmt.Errorf("connect to analysis engine: %w", err)
			}
			defer analysisClient.Close()

			// Call AnalyzeStock with a generous timeout
			analyzeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()

			resp, err := analysisClient.AnalyzeStock(analyzeCtx, &analysisv1.AnalyzeStockRequest{
				Symbol:           symbol,
				ForecastDays:     forecastDays,
				AvailableCapital: capital,
				RiskTolerance:    risk,
				HistoricalBars:   stockData.HistoricalBars,
				OptionChain:      stockData.OptionChain,
				CurrentQuote:     stockData.Quote,
			})
			if err != nil {
				return fmt.Errorf("analyze: %w", err)
			}

			// Print the report
			printAnalysisReport(resp, symbol, weeks)
			return nil
		},
	}

	cmd.Flags().IntVar(&weeks, "weeks", 2, "Forecast period in weeks")
	cmd.Flags().Float64Var(&capital, "capital", 50000, "Available capital for strategy sizing")
	cmd.Flags().StringVar(&risk, "risk", "moderate", "Risk tolerance: conservative, moderate, aggressive")
	cmd.Flags().BoolVar(&useWatchlist, "watchlist", false, "Run deep analysis for all watchlist symbols")
	cmd.Flags().StringVar(&analysisAddr, "analysis-addr", "localhost:50052", "Python analysis engine gRPC address")

	return cmd
}

// runWatchlistAnalysis runs full deep analysis sequentially for all watchlist symbols.
func runWatchlistAnalysis(ctx context.Context, forecastDays int32, capital float64, risk, analysisAddr string) error {
	// Open SQLite store
	store, err := sqlite.New(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close()

	// Get watchlist
	items, err := store.GetWatchlist(ctx)
	if err != nil {
		return fmt.Errorf("get watchlist: %w", err)
	}
	if len(items) == 0 {
		fmt.Println("Watchlist is empty. Use 'optix watch add AAPL TSLA' to add symbols.")
		return nil
	}

	weeks := int(forecastDays / 7)
	fmt.Printf("📋 Watchlist Deep Analysis — %d symbols\n", len(items))
	fmt.Printf("⏳ Connecting to IB TWS at %s:%d...\n", ibHost, ibPort)

	// Connect to IB TWS once for all symbols
	ibClient := ibkr.New(ibkr.Config{
		Host:     ibHost,
		Port:     ibPort,
		ClientID: 5, // dedicated ClientID for batch watchlist analysis
	})
	if err := ibClient.Connect(ctx); err != nil {
		return fmt.Errorf("connect to IB: %w", err)
	}
	defer ibClient.Disconnect()

	svc := server.NewMarketDataService(ibClient, store)

	// Connect to Python analysis engine once
	fmt.Printf("🔬 Analysis engine at %s\n", analysisAddr)
	analysisClient, err := analysis.NewClient(analysisAddr)
	if err != nil {
		return fmt.Errorf("connect to analysis engine: %w", err)
	}
	defer analysisClient.Close()

	// Process each symbol sequentially (IB pacing rules)
	for i, item := range items {
		sym := strings.ToUpper(item.Symbol)
		fmt.Printf("\n[%d/%d] Analyzing %s...\n", i+1, len(items), sym)

		stockData, fetchErr := fetchSymbolData(ctx, sym, svc, ibClient)
		if fetchErr != nil {
			fmt.Printf("  ⚠️  %s: fetch error: %v — skipping\n", sym, fetchErr)
			continue
		}

		analyzeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		resp, analyzeErr := analysisClient.AnalyzeStock(analyzeCtx, &analysisv1.AnalyzeStockRequest{
			Symbol:           sym,
			ForecastDays:     forecastDays,
			AvailableCapital: capital,
			RiskTolerance:    risk,
			HistoricalBars:   stockData.HistoricalBars,
			OptionChain:      stockData.OptionChain,
			CurrentQuote:     stockData.Quote,
		})
		cancel()

		if analyzeErr != nil {
			fmt.Printf("  ⚠️  %s: analysis error: %v — skipping\n", sym, analyzeErr)
			continue
		}

		printAnalysisReport(resp, sym, weeks)
	}

	fmt.Printf("\n✅ Watchlist analysis complete (%d symbols)\n", len(items))
	return nil
}

// fetchSymbolData delegates to server.FetchSymbolData (shared with the web UI).
func fetchSymbolData(ctx context.Context, symbol string, svc *server.MarketDataService, ibClient *ibkr.Client) (*analysisv1.SingleStockData, error) {
	return server.FetchSymbolData(ctx, symbol, svc, ibClient)
}

// ─── Report printing ──────────────────────────────────────────────────────────

const (
	lineWidth   = 65
	sectionSep  = "─────────────────────────────────────────────────────────────────"
	doubleSep   = "═════════════════════════════════════════════════════════════════"
)

func printAnalysisReport(resp *analysisv1.AnalyzeStockResponse, symbol string, weeks int) {
	if resp == nil {
		fmt.Println("No analysis data returned.")
		return
	}

	// ── Header ───────────────────────────────────────────────────────────────
	title := fmt.Sprintf("  %s  ─  Analysis Report (%d-Week Forecast)  ", symbol, weeks)
	fmt.Println()
	fmt.Println("╔" + strings.Repeat("═", lineWidth) + "╗")
	fmt.Printf("║%-*s║\n", lineWidth, center(title, lineWidth))
	fmt.Println("╚" + strings.Repeat("═", lineWidth) + "╝")

	// ── Stock Summary ─────────────────────────────────────────────────────────
	s := resp.Summary
	if s != nil {
		fmt.Println("\n📊  STOCK SUMMARY")
		fmt.Println(sectionSep)
		changeSign := "+"
		if s.Change < 0 {
			changeSign = ""
		}
		fmt.Printf("  %-14s $%.2f  (%s%.2f / %s%.2f%%)\n",
			"Price:", s.Price, changeSign, s.Change, changeSign, s.ChangePct)
		fmt.Printf("  %-14s $%.2f  ─  $%.2f\n", "52W Range:", s.Low_52W, s.High_52W)
		fmt.Printf("  %-14s %s  (avg 20d: %s)\n",
			"Volume:", fmtVol(s.TodayVolume), fmtVol(int64(s.AvgVolume_20D)))
	}

	// ── Technical Analysis ────────────────────────────────────────────────────
	t := resp.Technical
	if t != nil {
		trendUpper := strings.ToUpper(t.Trend)
		trendEmoji := trendEmoji(t.Trend)
		fmt.Printf("\n%s  TECHNICAL ANALYSIS  │  %s %s  (score %+.2f)\n",
			trendEmoji, trendUpper, trendLabel(t.Trend), t.TrendScore)
		fmt.Println(sectionSep)
		if t.TrendDescription != "" {
			fmt.Printf("  %s\n", t.TrendDescription)
		}
		fmt.Printf("  %-8s $%-10.2f  %-8s $%-10.2f  %-8s $%.2f\n",
			"MA20:", t.Ma_20, "MA50:", t.Ma_50, "MA200:", t.Ma_200)
		fmt.Printf("  RSI(14): %.1f   MACD: %+.2f   Signal: %+.2f   Hist: %+.2f\n",
			t.Rsi_14, t.Macd, t.MacdSignal, t.MacdHistogram)
		if t.BollingerUpper > 0 {
			fmt.Printf("  Bollinger Bands:  Upper $%.2f  │  Mid $%.2f  │  Lower $%.2f\n",
				t.BollingerUpper, t.BollingerMid, t.BollingerLower)
		}
		if len(t.SupportLevels) > 0 {
			fmt.Println("  Support Levels:")
			for _, sl := range t.SupportLevels {
				fmt.Printf("    • $%-8.2f  (%s, strength %.0f)\n", sl.Price, sl.Source, sl.Strength)
			}
		}
		if len(t.ResistanceLevels) > 0 {
			fmt.Println("  Resistance Levels:")
			for _, rl := range t.ResistanceLevels {
				fmt.Printf("    • $%-8.2f  (%s, strength %.0f)\n", rl.Price, rl.Source, rl.Strength)
			}
		}
	}

	// ── Options Analysis ──────────────────────────────────────────────────────
	o := resp.Options
	if o != nil {
		ivEnvLabel := strings.ToUpper(o.IvEnvironment)
		fmt.Printf("\n🎯  OPTIONS ANALYSIS  │  IV Env: %s\n", ivEnvLabel)
		fmt.Println(sectionSep)
		fmt.Printf("  HV/IV(20d): %.1f%%   IV Rank: %.1f%%   IV Pctile: %.1f%%\n",
			o.IvCurrent*100, o.IvRank, o.IvPercentile)
		if o.MaxPain > 0 {
			fmt.Printf("  Max Pain:   $%.2f  (expiry: %s)\n", o.MaxPain, o.MaxPainExpiry)
		} else {
			fmt.Println("  Max Pain:   N/A (no OI data from IB structure-only chain)")
		}
		fmt.Printf("  PCR (OI):   %.2f   PCR (Vol): %.2f\n", o.PcrOi, o.PcrVolume)
		if len(o.OiClusters) > 0 {
			fmt.Println("  OI Clusters:")
			for _, cl := range o.OiClusters {
				optLabel := "CALL"
				if cl.OptionType == marketdatav1.OptionType_OPTION_TYPE_PUT {
					optLabel = "PUT "
				}
				fmt.Printf("    • $%-8.0f %s  OI: %-6d  (%s)\n",
					cl.Strike, optLabel, cl.OpenInterest, cl.Significance)
			}
		}
	}

	// ── Market Outlook ────────────────────────────────────────────────────────
	ol := resp.Outlook
	if ol != nil {
		dirEmoji := trendEmoji(ol.Direction)
		fmt.Printf("\n%s  MARKET OUTLOOK  │  %s  (confidence: %.1f%%)\n",
			dirEmoji, strings.ToUpper(ol.Direction), ol.Confidence)
		fmt.Println(sectionSep)
		if ol.Rationale != "" {
			// word-wrap rationale at ~60 chars
			for _, line := range wrapText(ol.Rationale, 60) {
				fmt.Printf("  %s\n", line)
			}
		}
		fmt.Printf("\n  %d-Day Forecast Price Ranges:\n", ol.ForecastDays)
		fmt.Printf("    1σ (68%%):  $%.2f  ─  $%.2f\n", ol.RangeLow_1S, ol.RangeHigh_1S)
		fmt.Printf("    2σ (95%%):  $%.2f  ─  $%.2f\n", ol.RangeLow_2S, ol.RangeHigh_2S)
		if len(ol.RiskEvents) > 0 {
			fmt.Println("  Risk Events:")
			for _, re := range ol.RiskEvents {
				fmt.Printf("    ⚠️  %s\n", re)
			}
		}
	}

	// ── Strategy Recommendations ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println(doubleSep)
	fmt.Printf("%-*s\n", lineWidth, center("  STRATEGY RECOMMENDATIONS  ", lineWidth))
	fmt.Println(doubleSep)

	if len(resp.Strategies) == 0 {
		fmt.Println("  No strategy recommendations available.")
	} else {
		for i, strat := range resp.Strategies {
			printStrategy(i+1, strat)
		}
	}

	fmt.Println()
}

func printStrategy(rank int, strat *analysisv1.StrategyRecommendation) {
	if strat == nil {
		return
	}
	starLabel := "  "
	if strat.StrategyType != "none" {
		starLabel = "★ "
	}
	fmt.Printf("\n%s#%d  %s  [Score: %.0f/100]\n", starLabel, rank, strat.StrategyName, strat.Score)
	fmt.Printf("  %s\n", strings.Repeat("─", 50))

	// Legs
	if len(strat.Legs) > 0 {
		fmt.Print("  Legs:     ")
		legStrs := make([]string, 0, len(strat.Legs))
		for _, leg := range strat.Legs {
			dir := "Buy"
			if leg.Quantity < 0 {
				dir = "Sell"
			}
			otLabel := "C"
			if leg.OptionType == marketdatav1.OptionType_OPTION_TYPE_PUT {
				otLabel = "P"
			}
			expShort := leg.Expiration
			if len(expShort) >= 10 {
				expShort = expShort[5:10] // "MM-DD" from "YYYY-MM-DD"
			}
			legStrs = append(legStrs, fmt.Sprintf("%s %.0f%s(%s)", dir, leg.Strike, otLabel, expShort))
		}
		fmt.Println(strings.Join(legStrs, "  /  "))
	}

	fmt.Printf("  Credit:   $%.2f net\n", strat.NetCredit)
	fmt.Printf("  Max P/L:  +$%.0f  /  -$%.0f  │  R/R: %.2f\n",
		strat.MaxProfit, strat.MaxLoss, strat.RiskRewardRatio)
	fmt.Printf("  Prob Win: %.1f%%  │  Breakeven: $%.2f  │  Margin: $%.0f\n",
		strat.ProbabilityOfProfit, strat.BreakevenPrice, strat.MarginRequired)

	if strat.Rationale != "" {
		fmt.Printf("  Rationale: %s\n", strat.Rationale)
	}
	for _, w := range strat.RiskWarnings {
		fmt.Printf("  ⚠️  %s\n", w)
	}
}

// ─── Formatting helpers ───────────────────────────────────────────────────────

func center(s string, width int) string {
	if len(s) >= width {
		return s
	}
	pad := (width - len(s)) / 2
	return strings.Repeat(" ", pad) + s + strings.Repeat(" ", width-pad-len(s))
}

func fmtVol(v int64) string {
	if v == 0 {
		return "N/A"
	}
	switch {
	case v >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(v)/1e9)
	case v >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(v)/1e6)
	case v >= 1_000:
		return fmt.Sprintf("%.1fK", float64(v)/1e3)
	default:
		return fmt.Sprintf("%d", v)
	}
}

func trendEmoji(trend string) string {
	switch strings.ToLower(trend) {
	case "bullish":
		return "📈"
	case "bearish":
		return "📉"
	default:
		return "➡️ "
	}
}

func trendLabel(trend string) string {
	switch strings.ToLower(trend) {
	case "bullish":
		return "↑"
	case "bearish":
		return "↓"
	default:
		return "→"
	}
}

func wrapText(text string, width int) []string {
	words := strings.Fields(text)
	var lines []string
	var cur strings.Builder
	for _, w := range words {
		if cur.Len() > 0 && cur.Len()+1+len(w) > width {
			lines = append(lines, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(w)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}
