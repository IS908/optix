package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/IS908/optix/internal/eventintel"
	"github.com/spf13/cobra"
)

func newEventCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "event",
		Short: "Event cockpit — FOMC/CPI rate path, statement diff, historical patterns, sensitivity matrix",
		Long: `Event view data bundle for the Market Intel cockpit:
rate/yield proxy repricing, deterministic FOMC statement wording diff,
historical FOMC/CPI event-day patterns, and an asset sensitivity matrix.

Agents read this around scheduled macro events. M6 v1 is free-source-first:
market rows use yfinance labels and local deterministic event/statement
fixtures; IBKR exact contract mapping is intentionally left for a later adapter.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			svc := eventintel.NewDefaultService(pythonBin)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			b, err := svc.Bundle(ctx)
			if err != nil {
				return cliExit(fmt.Errorf("event: %w", err), exitGenericErr)
			}
			if strings.ToLower(format) == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(b)
			}
			renderEvent(b)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

func renderEvent(b eventintel.BundleDTO) {
	fmt.Println("═══ EVENT ═══")
	fmt.Println("\n利率/资产重定价:")
	for _, row := range b.Rates.Rows {
		if row.Missing {
			fmt.Printf("  %-6s 缺席 · %s\n", row.ID, row.Note)
			continue
		}
		fmt.Printf("  %-6s %+.2f (%+.2f%%) · %s/%s\n", row.ID, row.Change, row.ChangePct, row.Source, row.Basis)
	}

	fmt.Printf("\n声明 Diff: %s · hawkish %d / dovish %d\n", b.Diff.Verdict, b.Diff.HawkishHits, b.Diff.DovishHits)
	for _, sentence := range topSentences(b.Diff.Added, 2) {
		fmt.Printf("  + %s\n", sentence.Text)
	}
	for _, sentence := range topSentences(b.Diff.Removed, 2) {
		fmt.Printf("  - %s\n", sentence.Text)
	}

	fmt.Println("\n历史事件模式:")
	for _, row := range topPatternRows(b.Patterns.Rows, 4) {
		fmt.Printf("  %-6s n=%d event %+.2f%% next %+.2f%% consistency %.0f%%\n",
			row.ID, row.SampleN, row.AvgEventMovePct, row.AvgNextMovePct, row.DirectionalConsistency*100)
	}

	fmt.Println("\n敏感度矩阵:")
	for _, row := range topSensitivityRows(b.Sensitivity.Rows, 6) {
		fmt.Printf("  %-6s risk %+0.2f rates %+0.2f dollar %+0.2f\n", row.ID, row.RiskOn, row.RatesUp, row.DollarUp)
	}
}

func topSentences(in []eventintel.StatementSentence, n int) []eventintel.StatementSentence {
	if len(in) > n {
		return in[:n]
	}
	return in
}

func topPatternRows(in []eventintel.EventPatternRow, n int) []eventintel.EventPatternRow {
	if len(in) > n {
		return in[:n]
	}
	return in
}

func topSensitivityRows(in []eventintel.SensitivityRow, n int) []eventintel.SensitivityRow {
	if len(in) > n {
		return in[:n]
	}
	return in
}
