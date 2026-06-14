package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/postclose"
	"github.com/spf13/cobra"
)

func newPostcloseCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "postclose",
		Short: "Postclose cockpit — earnings, timeline, read-across, after-hours movers (no IBKR)",
		Long: `Postclose view data bundle for the Market Intel cockpit:
earnings quick read vs free yfinance EPS consensus, structured event timeline,
same-sector read-across edges, and combined regular-session plus after-hours
movers for watchlist ∪ a built-in liquid set.

Agents read this at the 16:30 对账 / 收盘后 checkpoint. No IBKR and no Python
gRPC engine required (yfinance subprocess only).`,
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

			svc, err := postclose.NewDefaultService(pythonBin)
			if err != nil {
				return cliExit(fmt.Errorf("postclose service: %w", err), exitGenericErr)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			watchlist, _ := store.GetWatchlist(ctx)
			b, err := svc.Bundle(ctx, watchlistSymbols(watchlist))
			if err != nil {
				return cliExit(fmt.Errorf("postclose: %w", err), exitGenericErr)
			}
			if strings.ToLower(format) == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(b)
			}
			renderPostclose(b)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

func renderPostclose(b postclose.BundleDTO) {
	fmt.Println("═══ POSTCLOSE ═══")
	fmt.Println("\n财报速递:")
	for _, r := range topReports(b.Earnings.Reports, 5) {
		fmt.Printf("  %-6s %-9s %s\n", r.Symbol, r.SurpriseLabel, r.EventTime.Format("2006-01-02 15:04"))
	}
	if len(b.Earnings.Reports) == 0 {
		fmt.Println("  暂无近期财报行")
	}

	fmt.Println("\n盘后异动:")
	for _, m := range topPostcloseMovers(b.Movers.Gainers, 5) {
		fmt.Printf("  ↑ %-6s 合并%+.2f%% 盘后%+.2f%%%s\n", m.Symbol, m.CombinedPct, m.AfterHoursPct, wlMark(m.Watchlist))
	}
	for _, m := range topPostcloseMovers(b.Movers.Losers, 5) {
		fmt.Printf("  ↓ %-6s 合并%+.2f%% 盘后%+.2f%%%s\n", m.Symbol, m.CombinedPct, m.AfterHoursPct, wlMark(m.Watchlist))
	}

	fmt.Printf("\nRead-across: %d edges\n", len(b.ReadAcross.Edges))
	for _, e := range topEdges(b.ReadAcross.Edges, 5) {
		fmt.Printf("  %s → %s · %s · strength %.0f%%\n", e.Driver, e.Peer, e.Direction, e.SignalStrength*100)
	}

	fmt.Printf("\n时间轴: %d events\n", len(b.Timeline.Events))
}

func topReports(in []postclose.EarningsReport, n int) []postclose.EarningsReport {
	if len(in) > n {
		return in[:n]
	}
	return in
}

func topPostcloseMovers(in []postclose.Mover, n int) []postclose.Mover {
	if len(in) > n {
		return in[:n]
	}
	return in
}

func topEdges(in []postclose.ReadAcrossEdge, n int) []postclose.ReadAcrossEdge {
	if len(in) > n {
		return in[:n]
	}
	return in
}
