package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/IS908/optix/internal/shockintel"
	"github.com/spf13/cobra"
)

func newShockCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "shock",
		Short: "Shock cockpit — regime trigger, fingerprints, analogs, liquidity state",
		Long: `Shock view data bundle for the Market Intel cockpit:
regime trigger, supply/demand/liquidity/policy shock fingerprints,
historical analog matching, and ETF liquidity state.

Agents read this during abnormal cross-asset moves. M7 v1 is IBKR-preferred
by contract but free-source runnable: yfinance supplies quotes/bars while
depth and exact option stress degrade explicitly until an IBKR adapter is wired.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			svc := shockintel.NewIBKRPreferredService(ibHost, ibPort, pythonBin)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			b, err := svc.Bundle(ctx)
			if err != nil {
				return cliExit(fmt.Errorf("shock: %w", err), exitGenericErr)
			}
			if strings.ToLower(format) == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(b)
			}
			renderShock(b)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

func renderShock(b shockintel.BundleDTO) {
	fmt.Println("═══ SHOCK ═══")
	fmt.Printf("\nRegime: %s · score %.0f · VIX Δ/10 %.1f\n", b.Regime.State, b.Regime.Score, b.Regime.VIXChangeRatio)
	for _, c := range topRegimeConfirmations(b.Regime.Confirmations, 5) {
		fmt.Printf("  %-5s %-10s %+5.2f%% · %s/%s\n", c.ID, c.Dimension, c.ChangePct, c.Source, c.Basis)
	}

	fmt.Println("\n冲击指纹:")
	for _, row := range b.Fingerprint.Rows {
		active := " "
		if row.Active {
			active = "*"
		}
		fmt.Printf("  %s %-10s score %.0f confidence %.0f%%\n", active, row.Kind, row.Score, row.Confidence*100)
	}

	fmt.Println("\n历史类比:")
	for _, row := range topAnalogRows(b.Analogs.Rows, 3) {
		fmt.Printf("  %-24s %.0f%% · %s\n", row.Name, row.Similarity*100, row.NextSessionBias)
	}

	fmt.Println("\n流动性:")
	for _, row := range topLiquidityRows(b.Liquidity.Rows, 6) {
		if row.Missing {
			fmt.Printf("  %-5s missing · %s\n", row.ID, row.Note)
			continue
		}
		fmt.Printf("  %-5s %-8s spread %.1fbp z %.1f depth:%t\n", row.ID, row.State, row.SpreadBps, row.SpreadZ, row.DepthAvailable)
	}
}

func topRegimeConfirmations(in []shockintel.RegimeConfirmation, n int) []shockintel.RegimeConfirmation {
	if len(in) > n {
		return in[:n]
	}
	return in
}

func topAnalogRows(in []shockintel.AnalogRow, n int) []shockintel.AnalogRow {
	if len(in) > n {
		return in[:n]
	}
	return in
}

func topLiquidityRows(in []shockintel.LiquidityRow, n int) []shockintel.LiquidityRow {
	if len(in) > n {
		return in[:n]
	}
	return in
}
