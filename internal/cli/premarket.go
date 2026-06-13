package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/premarket"
	"github.com/IS908/optix/pkg/model"
	"github.com/spf13/cobra"
)

func newPremarketCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "premarket",
		Short: "Premarket cockpit — overnight chain, gap-fill stats, movers, sentiment (no IBKR)",
		Long: `Premarket view data bundle for the Market Intel cockpit:
overnight transmission chain (N225→TSMC→SX5E→ES), implied open + historical
gap-fill statistics (SPX), premarket movers + volume ratio (watchlist ∪ a
built-in liquid set), and sentiment positioning (Put/Call + VIX term premium).

Agents read this at the 08:00 剧本 checkpoint for premarket context. No IBKR
and no Python gRPC engine required (yfinance subprocess only).`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(fmt.Errorf("open database: %w", err), exitSQLiteErr)
			}
			RegisterCleanup(store)
			defer store.Close()

			svc := premarket.NewService(premarket.NewMarketAdapter(pythonBin), store)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			watchlist, _ := store.GetWatchlist(ctx)
			b, err := svc.Bundle(ctx, watchlistSymbols(watchlist))
			if err != nil {
				return cliExit(fmt.Errorf("premarket: %w", err), exitGenericErr)
			}
			if strings.ToLower(format) == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(b)
			}
			renderPremarket(b)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

func renderPremarket(b premarket.BundleDTO) {
	fmt.Println("═══ PREMARKET ═══")
	fmt.Printf("\n隔夜传导: %s\n", b.Overnight.Consistency.Note)
	for _, l := range b.Overnight.Links {
		fmt.Printf("  %-12s %+.2f%%\n", l.Label, l.Pct)
	}
	fmt.Printf("\n隐含跳空: %s %+.2f%% (%s 档) · 历史回补率 %.0f%% (n=%d)\n",
		b.Gaps.Symbol, b.Gaps.ImpliedGapPct, b.Gaps.Band, b.Gaps.HistFillRate*100, b.Gaps.SampleN)
	fmt.Printf("\n盘前异动 (%s):\n", b.Movers.UniverseNote)
	for _, m := range topN(b.Movers.Gainers, 5) {
		fmt.Printf("  ↑ %-6s %+.2f%% ·量比 %.1fx%s\n", m.Symbol, m.Pct, m.VolRatio, wlMark(m.Watchlist))
	}
	for _, m := range topN(b.Movers.Losers, 5) {
		fmt.Printf("  ↓ %-6s %+.2f%% ·量比 %.1fx%s\n", m.Symbol, m.Pct, m.VolRatio, wlMark(m.Watchlist))
	}
	pc := "P/C 不可用"
	if b.Sentiment.PCAvailable {
		pc = fmt.Sprintf("P/C(OI) %.2f", b.Sentiment.PCOI)
	}
	fmt.Printf("\n情绪: %s · VIX 期限 %.2f · regime %s\n", pc, b.Sentiment.VIXTermPremium, b.Sentiment.Regime)
}

func topN(m []premarket.Mover, n int) []premarket.Mover {
	if len(m) > n {
		return m[:n]
	}
	return m
}

func wlMark(wl bool) string {
	if wl {
		return " ★"
	}
	return ""
}

func watchlistSymbols(items []model.WatchlistItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Symbol)
	}
	return out
}
