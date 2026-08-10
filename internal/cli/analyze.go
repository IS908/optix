package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
	marketdatav1 "github.com/IS908/optix/gen/go/optix/marketdata/v1"
	"github.com/IS908/optix/internal/analysis"
	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/server"
	"github.com/spf13/cobra"
)

func newAnalyzeCmd() *cobra.Command {
	var weeks int
	var capital float64
	var risk string
	var useWatchlist bool
	var analysisAddr string
	var withOI bool
	var expiry string
	var format string

	cmd := &cobra.Command{
		Use:   "analyze [symbol]",
		Short: "Run full stock analysis with options strategy recommendations",
		Long: `Analyze a stock comprehensively: technical analysis, options data (OI, IV, Max Pain),
price range forecast, and sell-side options strategy recommendations.

Examples:
  optix analyze AAPL
  optix analyze AAPL --weeks=2 --capital=50000
  optix analyze AAPL --risk=conservative
  optix analyze --watchlist --capital=100000
  optix analyze AAPL --format json
  optix analyze --watchlist --format json

--format json emits a single stable JSON document (or, with --watchlist, an
object with a per-symbol "results" array) instead of the text report.
Diagnostics and progress messages move to stderr so stdout stays valid JSON.`,
		// We render our own errors (FormatExpiryError) and don't want cobra
		// dumping a usage banner or duplicating the "Error:" prefix on every
		// runtime failure.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			format = strings.ToLower(format)
			logw := os.Stdout
			if format == "json" {
				logw = os.Stderr
			}

			analysisAddr = resolveAnalysisAddr(cmd, analysisAddr)
			ctx := context.Background()
			forecastDays := int32(weeks * 7)

			if useWatchlist {
				return runWatchlistAnalysis(ctx, forecastDays, capital, risk, analysisAddr, format)
			}

			if len(args) == 0 {
				return fmt.Errorf("please specify a symbol or use --watchlist")
			}

			symbol := strings.ToUpper(args[0])

			expiryCompact, err := parseExpiryFlag(expiry, time.Now())
			if err != nil {
				return err
			}
			if expiryCompact != "" && !withOI {
				return fmt.Errorf("--expiry requires --with-oi")
			}

			fmt.Fprintf(logw, "⏳ Connecting to market data source...\n")

			// Open SQLite store for caching
			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(fmt.Errorf("open database: %w", err), exitSQLiteErr)
			}
			RegisterCleanup(store)
			defer store.Close()

			// Connect to broker (IBKR with yfinance fallback)
			b := factory.NewWithFallback(ibkr.Config{
				Host:     ibHost,
				Port:     ibPort,
				ClientID: 2,
			}, pythonBin)
			if err := b.Connect(ctx); err != nil {
				return cliExit(fmt.Errorf("connect to broker: %w", err), exitIBKRUnreachable)
			}
			defer b.Disconnect()
			RegisterBrokerCleanup(b)
			fmt.Fprintln(logw, b.SourceBanner())

			// Create MarketDataService with SQLite caching
			svc := server.NewMarketDataService(b, store)

			// Fetch all data for this symbol
			if withOI {
				fmt.Fprintf(logw, "📊 Fetching data for %s (with per-contract OI — this may take ~10–30s)...\n", symbol)
			} else {
				fmt.Fprintf(logw, "📊 Fetching data for %s...\n", symbol)
			}
			stockData, err := server.FetchSymbolDataOpt(ctx, symbol, svc, server.FetchOptions{
				WithOI: withOI,
				Expiry: expiryCompact,
			})
			if err != nil {
				var miss *broker.ErrExpiryNotAvailable
				if errors.As(err, &miss) {
					tmpl := fmt.Sprintf("optix analyze %s --with-oi --expiry %%s", symbol)
					return errors.New(FormatExpiryError(miss.Underlying, miss.Requested, miss.Available, tmpl))
				}
				return cliExit(fmt.Errorf("fetch data: %w", err), exitIBKRUnreachable)
			}

			// Connect to Python analysis engine
			fmt.Fprintf(logw, "🔬 Running analysis engine at %s...\n", analysisAddr)
			analysisClient, err := analysis.NewClient(analysisAddr)
			if err != nil {
				return cliExit(fmt.Errorf("connect to analysis engine: %w", err), exitGenericErr)
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
				return cliExit(fmt.Errorf("analyze: %w", err), exitGenericErr)
			}

			if format == "json" {
				out := buildAnalyzeOutput(resp, symbol, weeks, forecastDays, b.SourceName(), expiryCompact != "")
				if err := writeJSON(os.Stdout, out); err != nil {
					return cliExit(fmt.Errorf("write json: %w", err), exitGenericErr)
				}
				return nil
			}

			// Print the report
			printAnalysisReport(resp, symbol, weeks, expiryCompact != "")
			return nil
		},
	}

	cmd.Flags().IntVar(&weeks, "weeks", 2, "Forecast period in weeks")
	cmd.Flags().Float64Var(&capital, "capital", 50000, "Available capital for strategy sizing")
	cmd.Flags().StringVar(&risk, "risk", "moderate", "Risk tolerance: conservative, moderate, aggressive")
	cmd.Flags().BoolVar(&useWatchlist, "watchlist", false, "Run deep analysis for all watchlist symbols")
	cmd.Flags().StringVar(&analysisAddr, "analysis-addr", defaultAnalysisAddr, "Python analysis engine gRPC address")
	cmd.Flags().BoolVar(&withOI, "with-oi", false, "Fetch per-contract Open Interest for the nearest expiry (requires OI-capable broker; enables Max Pain). Adds ~10–30s.")
	cmd.Flags().StringVar(&expiry, "expiry", "", "Specific option expiration YYYY-MM-DD (default: nearest); requires --with-oi")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")

	return cmd
}

// parseExpiryFlag validates a --expiry flag value and returns the canonical
// YYYYMMDD form. Empty input yields ("", nil) — caller treats as "nearest".
// Today is allowed; dates in the past are rejected. `now` is injected for
// test determinism.
func parseExpiryFlag(value string, now time.Time) (string, error) {
	if value == "" {
		return "", nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", fmt.Errorf("invalid --expiry %q: use YYYY-MM-DD", value)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if t.Before(today) {
		return "", fmt.Errorf("expiry %s is in the past; pick today or a future date", value)
	}
	return t.Format("20060102"), nil
}

// maxPainExpiryAnnotation renders the expiry the Python engine reported it
// computed Max Pain over. The Python servicer sends back the value already
// formatted as YYYY-MM-DD (see formatIBExpiry in server/fetch.go); we still
// pass it through dashed() so an unexpected YYYYMMDD survives gracefully.
// Empty/unknown values surface as "unknown" so the line stays honest.
func maxPainExpiryAnnotation(maxPainExpiry string) string {
	if maxPainExpiry == "" {
		return "unknown"
	}
	return dashed(maxPainExpiry)
}

// runWatchlistAnalysis runs full deep analysis sequentially for all watchlist
// symbols. format must already be validated/lowercased by the caller
// (validateOutputFormat); "json" routes all diagnostics/progress to stderr
// and emits one stable analyzeWatchlistOutput JSON document to stdout —
// per-symbol fetch/analyze failures become structured {symbol, error}
// entries rather than aborting the batch. Exit-code semantics are unchanged
// from text mode: see watchlistAnalysisExit, called identically regardless
// of format.
func runWatchlistAnalysis(ctx context.Context, forecastDays int32, capital float64, risk, analysisAddr, format string) error {
	logw := os.Stdout
	if format == "json" {
		logw = os.Stderr
	}

	// Open SQLite store
	store, err := sqlite.New(dbPath)
	if err != nil {
		return cliExit(fmt.Errorf("open database: %w", err), exitSQLiteErr)
	}
	RegisterCleanup(store)
	defer store.Close()

	// Get watchlist
	items, err := store.GetWatchlist(ctx)
	if err != nil {
		return cliExit(fmt.Errorf("get watchlist: %w", err), exitSQLiteErr)
	}

	weeks := int(forecastDays / 7)

	if len(items) == 0 {
		if format == "json" {
			return writeJSON(os.Stdout, analyzeWatchlistOutput{
				Weeks:        weeks,
				ForecastDays: forecastDays,
				GeneratedAt:  time.Now().UTC(),
				Results:      []analyzeWatchlistEntry{},
			})
		}
		fmt.Println("Watchlist is empty. Use 'optix watch add AAPL TSLA' to add symbols.")
		return nil
	}

	fmt.Fprintf(logw, "📋 Watchlist Deep Analysis — %d symbols\n", len(items))
	fmt.Fprintf(logw, "⏳ Connecting to market data source...\n")

	// Connect to broker (IBKR with yfinance fallback)
	b := factory.NewWithFallback(ibkr.Config{
		Host:     ibHost,
		Port:     ibPort,
		ClientID: 6,
	}, pythonBin)
	if err := b.Connect(ctx); err != nil {
		return cliExit(fmt.Errorf("connect to broker: %w", err), exitIBKRUnreachable)
	}
	defer b.Disconnect()
	RegisterBrokerCleanup(b)
	fmt.Fprintln(logw, b.SourceBanner())

	svc := server.NewMarketDataService(b, store)

	// Connect to Python analysis engine once
	fmt.Fprintf(logw, "🔬 Analysis engine at %s\n", analysisAddr)
	analysisClient, err := analysis.NewClient(analysisAddr)
	if err != nil {
		return cliExit(fmt.Errorf("connect to analysis engine: %w", err), exitGenericErr)
	}
	defer analysisClient.Close()

	// Process each symbol sequentially (IB pacing rules)
	var successes int
	var firstFetchErr error
	var firstAnalyzeErr error
	entries := make([]analyzeWatchlistEntry, 0, len(items))
	for i, item := range items {
		sym := strings.ToUpper(item.Symbol)
		fmt.Fprintf(logw, "\n[%d/%d] Analyzing %s...\n", i+1, len(items), sym)

		stockData, fetchErr := fetchSymbolData(ctx, sym, svc)
		if fetchErr != nil {
			if firstFetchErr == nil {
				firstFetchErr = fetchErr
			}
			fmt.Fprintf(logw, "  ⚠️  %s: fetch error: %v — skipping\n", sym, fetchErr)
			entries = append(entries, analyzeWatchlistEntry{Symbol: sym, Error: fmt.Sprintf("fetch error: %v", fetchErr)})
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
			if firstAnalyzeErr == nil {
				firstAnalyzeErr = analyzeErr
			}
			fmt.Fprintf(logw, "  ⚠️  %s: analysis error: %v — skipping\n", sym, analyzeErr)
			entries = append(entries, analyzeWatchlistEntry{Symbol: sym, Error: fmt.Sprintf("analysis error: %v", analyzeErr)})
			continue
		}

		if format == "json" {
			entries = append(entries, analyzeWatchlistEntry{
				Symbol:   sym,
				Analysis: buildAnalyzeOutput(resp, sym, weeks, forecastDays, b.SourceName(), false),
			})
		} else {
			printAnalysisReport(resp, sym, weeks, false)
		}
		successes++
	}

	if format == "json" {
		if err := writeJSON(os.Stdout, analyzeWatchlistOutput{
			Weeks:        weeks,
			ForecastDays: forecastDays,
			Source:       b.SourceName(),
			GeneratedAt:  time.Now().UTC(),
			Results:      entries,
		}); err != nil {
			return cliExit(fmt.Errorf("write json: %w", err), exitGenericErr)
		}
	}

	if err := watchlistAnalysisExit(successes, firstFetchErr, firstAnalyzeErr); err != nil {
		return err
	}

	fmt.Fprintf(logw, "\n✅ Watchlist analysis complete (%d symbols)\n", len(items))
	return nil
}

func watchlistAnalysisExit(successes int, firstFetchErr, firstAnalyzeErr error) error {
	if successes > 0 {
		return nil
	}
	if firstAnalyzeErr != nil {
		return cliExit(fmt.Errorf("all watchlist analyses failed; first error: %w", firstAnalyzeErr), exitGenericErr)
	}
	if firstFetchErr != nil {
		return cliExit(fmt.Errorf("all watchlist fetches failed; first error: %w", firstFetchErr), exitIBKRUnreachable)
	}
	return nil
}

// fetchSymbolData delegates to server.FetchSymbolData (shared with the web UI).
func fetchSymbolData(ctx context.Context, symbol string, svc *server.MarketDataService) (*analysisv1.SingleStockData, error) {
	return server.FetchSymbolData(ctx, symbol, svc)
}

// ─── Report printing ──────────────────────────────────────────────────────────

const (
	lineWidth  = 65
	sectionSep = "─────────────────────────────────────────────────────────────────"
	doubleSep  = "═════════════════════════════════════════════════════════════════"
)

func printAnalysisReport(resp *analysisv1.AnalyzeStockResponse, symbol string, weeks int, userRequestedExpiry bool) {
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
			annotation := maxPainExpiryAnnotation(o.MaxPainExpiry)
			if userRequestedExpiry {
				annotation += ", requested"
			}
			fmt.Printf("  Max Pain:   $%.2f  (expiry %s)\n", o.MaxPain, annotation)
		} else {
			fmt.Println("  Max Pain:   N/A (rerun with --with-oi to fetch per-contract Open Interest)")
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
