package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/internal/server"
	"github.com/spf13/cobra"
)

// newPortfolioCmd is the parent command for account-level (vs single-name)
// risk views. v2.0 Phase 1 ships `concentration`; `greeks` and `stress` land
// in Phase 2/3.
func newPortfolioCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "portfolio",
		Short: "Account-level risk views (concentration, greeks, stress)",
		Long: `Account-level (vs single-name) risk views.

Phase 1 ships concentration analysis: per-underlying weight rollup, sector
grouping, Top-N exposure, HHI, and threshold-based flagging. Future phases
will add Greeks aggregation and stress testing — see
docs/v2.0-portfolio-risk-layer.md for the roadmap.`,
	}
	cmd.AddCommand(newPortfolioConcentrationCmd())
	return cmd
}

func newPortfolioConcentrationCmd() *cobra.Command {
	var (
		netLiqUSD     float64
		thresholdWarn float64
		thresholdRed  float64
		topN          int
		jsonOut       string
		sectorsFile   string
	)

	cmd := &cobra.Command{
		Use:   "concentration",
		Short: "Show portfolio concentration: per-name and per-sector weights with threshold flags",
		Long: `Compute portfolio concentration metrics across all account holdings.

For each underlying ticker, aggregates stock + option exposure (by |market value|)
and reports the percentage of net liquidating value (NLV). Single-name positions
exceeding configurable thresholds (default: 10% yellow, 20% red) are flagged.

Phase 1 caveat: optix does not yet read NLV from IBKR account summary. Pass
--net-liq-usd to anchor the weight calculation. If omitted, the report uses
the sum of |market value| as the denominator (deployed_pct will then be 100%,
which is signalled to the reader). True NLV integration lands in v2.1.`,
		Example: `  optix portfolio concentration --net-liq-usd 354477
  optix portfolio concentration --net-liq-usd 354477 --threshold-red 25 --top-n 15
  optix portfolio concentration --net-liq-usd 354477 --json /tmp/snap.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			store, err := sqlite.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			RegisterCleanup(store)
			defer store.Close()

			b := factory.NewWithFallback(ibkr.Config{
				Host:     ibHost,
				Port:     ibPort,
				ClientID: 4,
			}, pythonBin)
			if err := b.Connect(ctx); err != nil {
				return fmt.Errorf("connect to broker: %w", err)
			}
			defer b.Disconnect()
			fmt.Println(b.SourceBanner())

			market := server.NewMarketDataService(b, store)
			acct := server.NewAccountService(b, market)

			positions, err := acct.GetPositions(ctx)
			if err != nil {
				if errors.Is(err, broker.ErrAccountNotSupported) {
					return fmt.Errorf("账户数据需要 IBKR 连接，当前已回退到 Yahoo Finance（无账户接口）。请确认 TWS/Gateway 在运行")
				}
				return fmt.Errorf("get positions: %w", err)
			}

			// Load sector map. Empty/missing file → all tickers map to "unclassified".
			sm, err := portfolio.LoadSectorMap(sectorsFile)
			if err != nil {
				// Non-fatal: continue with empty mapping rather than fail the whole report.
				fmt.Fprintf(os.Stderr, "warn: %v (continuing with unclassified sector)\n", err)
				sm = &portfolio.SectorMap{
					SectorLabels:  map[string]string{},
					TickerSectors: map[string]string{},
				}
			}

			cfg := portfolio.DefaultConfig()
			if thresholdWarn > 0 {
				cfg.WarnPct = thresholdWarn
			}
			if thresholdRed > 0 {
				cfg.RedPct = thresholdRed
			}
			if topN > 0 {
				cfg.TopN = topN
			}

			// Phase 1 NLV fallback: if the user didn't pass --net-liq-usd, anchor
			// against the sum of |MV|. The report renders deployed=100%, which is
			// the visual cue that the denominator is a fallback rather than truth.
			anchorNLV := netLiqUSD
			if anchorNLV <= 0 {
				for _, p := range positions {
					if p.MarketValue >= 0 {
						anchorNLV += p.MarketValue
					} else {
						anchorNLV -= p.MarketValue
					}
				}
				fmt.Fprintln(os.Stderr, "warn: --net-liq-usd not provided; using sum(|MV|) as fallback NLV (cash + non-MV-bearing holdings will be missing)")
			}

			report := portfolio.Compute(positions, anchorNLV, cfg, sm)
			report.Render(os.Stdout)

			if jsonOut != "" {
				data, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return fmt.Errorf("marshal report: %w", err)
				}
				if err := os.WriteFile(jsonOut, data, 0o644); err != nil {
					return fmt.Errorf("write json: %w", err)
				}
				fmt.Fprintf(os.Stderr, "\nJSON snapshot written to %s\n", jsonOut)
			}

			return nil
		},
	}

	cmd.Flags().Float64Var(&netLiqUSD, "net-liq-usd", 0, "Net Liq value in USD (anchors weight calc). Omit to use sum(|MV|) fallback.")
	cmd.Flags().Float64Var(&thresholdWarn, "threshold-warn", 0, "Yellow flag threshold (% of NLV); default 10")
	cmd.Flags().Float64Var(&thresholdRed, "threshold-red", 0, "Red flag threshold (% of NLV); default 20")
	cmd.Flags().IntVar(&topN, "top-n", 0, "Top-N rollup count; default 10")
	cmd.Flags().StringVar(&jsonOut, "json", "", "Also write the full report as JSON to this path (for cron consumers)")
	cmd.Flags().StringVar(&sectorsFile, "sectors-file", "./configs/sectors.json", "Path to sector mapping JSON")

	return cmd
}
