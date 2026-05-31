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

// portfolioClientID is the IBKR ClientID used by `optix portfolio …`.
// Picked from the free slots in the matrix to avoid collision with positions
// (4), max-pain (8), or the analyze (2 / 6) and dashboard (3) paths. See
// issue #47 — the original v0.5.0 release used 4, which silently failed when
// run concurrently with `optix positions`.
const portfolioClientID = 5

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
	cmd.AddCommand(newPortfolioGreeksCmd())
	return cmd
}

// validateCurrencyFlags enforces the dual-currency display contract: the
// SGD net-liq and the USD→SGD rate are only meaningful together. Both must be
// positive, or both omitted. A negative value is rejected explicitly (rather
// than silently treated as "unset") so the error names the real problem.
func validateCurrencyFlags(netLiqSGD, fxUSDtoSGD float64) error {
	if netLiqSGD < 0 || fxUSDtoSGD < 0 {
		return fmt.Errorf("--net-liq-sgd and --fx-usd-sgd must be positive (got net-liq-sgd=%g, fx-usd-sgd=%g)",
			netLiqSGD, fxUSDtoSGD)
	}
	if (netLiqSGD > 0) != (fxUSDtoSGD > 0) {
		return fmt.Errorf("--net-liq-sgd and --fx-usd-sgd must be passed together (or neither)")
	}
	return nil
}

func newPortfolioConcentrationCmd() *cobra.Command {
	var (
		netLiqUSD     float64
		netLiqSGD     float64
		fxUSDtoSGD    float64
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
			// Flag validation FIRST — before any expensive broker/DB work.
			// A user with mistyped SGD/FX flags shouldn't pay the IBKR
			// round-trip cost only to be told their input is bad.
			if err := validateCurrencyFlags(netLiqSGD, fxUSDtoSGD); err != nil {
				return err
			}

			ctx := context.Background()

			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(fmt.Errorf("open database: %w", err), exitSQLiteErr)
			}
			RegisterCleanup(store)
			defer store.Close()

			b := factory.NewWithFallback(ibkr.Config{
				Host:     ibHost,
				Port:     ibPort,
				ClientID: portfolioClientID,
			}, pythonBin)
			if err := b.Connect(ctx); err != nil {
				return cliExit(fmt.Errorf("connect to broker: %w", err), exitIBKRUnreachable)
			}
			defer b.Disconnect()
			fmt.Println(b.SourceBanner())

			market := server.NewMarketDataService(b, store)
			acct := server.NewAccountService(b, market)

			positions, err := acct.GetPositions(ctx)
			if err != nil {
				if errors.Is(err, broker.ErrAccountNotSupported) {
					return cliExit(fmt.Errorf("账户数据需要 IBKR 连接，当前已回退到 Yahoo Finance（无账户接口）。请确认 TWS/Gateway 在运行"), exitIBKRUnreachable)
				}
				return cliExit(fmt.Errorf("get positions: %w", err), exitIBKRUnreachable)
			}

			// Resolve sector map via the search chain: explicit flag → env →
			// release-bundle configs → repo-relative configs → embedded
			// default. The embedded fallback guarantees the sector view is
			// never silently empty just because the cwd lacks ./configs/
			// (the v0.5.0 regression — see issue #48).
			sm, source, err := portfolio.ResolveSectorMap(sectorsFile)
			if err != nil {
				// Only an explicit override path can fail loudly here; the
				// embedded fallback always succeeds.
				return fmt.Errorf("resolve sector map: %w", err)
			}
			// Tell the user where the map came from whenever it isn't the
			// default auto-discovered location: an explicit --sectors-file, the
			// $OPTIX_SECTORS_FILE override, or the embedded fallback. All three
			// are cases where the user benefits from confirmation of the source.
			if source == "<embedded>" || sectorsFile != "" || os.Getenv("OPTIX_SECTORS_FILE") != "" {
				fmt.Fprintf(os.Stderr, "info: sector map source: %s\n", source)
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
			// against FallbackNLV (sum of |MV| over USD, non-residual legs). It
			// applies the same exclusions as Compute, so deployed_pct renders
			// 100% — the visual cue that the denominator is a fallback rather
			// than truth — even when a non-USD or residual holding is present.
			anchorNLV := netLiqUSD
			if anchorNLV <= 0 {
				anchorNLV = portfolio.FallbackNLV(positions)
				fmt.Fprintln(os.Stderr, "warn: --net-liq-usd not provided; using sum(|MV|) as fallback NLV (cash + non-MV-bearing holdings will be missing)")
			}

			report := portfolio.Compute(positions, anchorNLV, cfg, sm)

			// Wire up the SGD/FX display block (the XOR check at the top of
			// RunE has already validated they're both set or both unset).
			if netLiqSGD > 0 && fxUSDtoSGD > 0 {
				report.NetLiqSGD = netLiqSGD
				report.FXUSDtoSGD = fxUSDtoSGD
			}

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
	cmd.Flags().Float64Var(&netLiqSGD, "net-liq-sgd", 0, "Net Liq value in SGD, for dual-currency display (must be passed with --fx-usd-sgd). Manual until IBKR account-summary integration ships.")
	cmd.Flags().Float64Var(&fxUSDtoSGD, "fx-usd-sgd", 0, "USD→SGD exchange rate, for dual-currency display (must be passed with --net-liq-sgd).")
	cmd.Flags().Float64Var(&thresholdWarn, "threshold-warn", 0, "Yellow flag threshold (% of NLV); default 10")
	cmd.Flags().Float64Var(&thresholdRed, "threshold-red", 0, "Red flag threshold (% of NLV); default 20")
	cmd.Flags().IntVar(&topN, "top-n", 0, "Top-N rollup count; default 10")
	cmd.Flags().StringVar(&jsonOut, "json", "", "Also write the full report as JSON to this path (for cron consumers)")
	cmd.Flags().StringVar(&sectorsFile, "sectors-file", "", "Path to sector mapping JSON. Default search: $OPTIX_SECTORS_FILE → <bin-dir>/../configs/sectors.json → ./configs/sectors.json → embedded fallback (binary always has a copy).")

	return cmd
}
