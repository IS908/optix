package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/IS908/optix/internal/analysis"
	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/internal/server"
	"github.com/spf13/cobra"
)

// defaultRiskFreeRate is the Black-Scholes r when neither config nor flag sets
// it. Greeks are insensitive to r; this is the current US short-end T-bill.
const defaultRiskFreeRate = 0.043

func validateGroupBy(by string) error {
	if by != "underlying" && by != "sector" {
		return fmt.Errorf("invalid --by %q: use underlying or sector", by)
	}
	return nil
}

func newPortfolioGreeksCmd() *cobra.Command {
	var (
		groupBy      string
		netLiqUSD    float64
		riskFreeRate float64
		sectorsFile  string
		jsonOut      string
		analysisAddr string
	)
	cmd := &cobra.Command{
		Use:   "greeks",
		Short: "Aggregate portfolio Greeks (Δ/Γ/Vega/Θ) by underlying or sector",
		Long: `Aggregate per-leg option Greeks across all account holdings into
position-level dollar Greeks. Net Δ is delta-adjusted shares; Dollar Δ is the
USD exposure per +1% spot move; Vega is USD per +1% IV; Θ is USD per day.

Requires IBKR (account data has no Yahoo Finance fallback) and the Python
analysis engine for Black-Scholes pricing. Legs whose IV can't be resolved
from the option chain or inverted from the mark are skipped and listed.`,
		Example: `  optix portfolio greeks --net-liq-usd 354477
  optix portfolio greeks --by sector --json /tmp/greeks.json`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateGroupBy(groupBy); err != nil {
				return err
			}
			if riskFreeRate <= 0 {
				riskFreeRate = defaultRiskFreeRate
			}
			ctx := context.Background()

			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(fmt.Errorf("open database: %w", err), exitSQLiteErr)
			}
			RegisterCleanup(store)
			defer store.Close()

			b := factory.NewWithFallback(ibkr.Config{Host: ibHost, Port: ibPort, ClientID: portfolioClientID}, pythonBin)
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

			ac, err := analysis.NewClient(analysisAddr)
			if err != nil {
				return cliExit(fmt.Errorf("connect analysis engine at %s: %w", analysisAddr, err), exitGenericErr)
			}
			defer ac.Close()

			sm, source, err := portfolio.ResolveSectorMap(sectorsFile)
			if err != nil {
				return fmt.Errorf("resolve sector map: %w", err)
			}
			if source == "<embedded>" || sectorsFile != "" || os.Getenv("OPTIX_SECTORS_FILE") != "" {
				fmt.Fprintf(os.Stderr, "info: sector map source: %s\n", source)
			}

			anchorNLV := netLiqUSD
			if anchorNLV <= 0 {
				anchorNLV = portfolio.FallbackNLV(positions)
				fmt.Fprintln(os.Stderr, "warn: --net-liq-usd not provided; using sum(|MV|) as fallback NLV")
			}

			var pricer portfolio.OptionPricer = analysis.GreeksPricer{Client: ac}
			report, err := portfolio.AggregateGreeks(ctx, positions, portfolio.GreeksOptions{
				GroupBy: groupBy, NetLiqUSD: anchorNLV, RiskFreeRate: riskFreeRate, AsOf: time.Now().UTC(),
			}, pricer, market, sm)
			if err != nil {
				return cliExit(fmt.Errorf("aggregate greeks: %w", err), exitGenericErr)
			}

			portfolio.RenderGreeks(report, os.Stdout)

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
	cmd.Flags().StringVar(&groupBy, "by", "underlying", "Group by: underlying | sector")
	cmd.Flags().Float64Var(&netLiqUSD, "net-liq-usd", 0, "Net Liq value in USD (anchors weights). Omit to use sum(|MV|) fallback.")
	cmd.Flags().Float64Var(&riskFreeRate, "risk-free-rate", 0, "Risk-free rate for Black-Scholes (default 0.043)")
	cmd.Flags().StringVar(&sectorsFile, "sectors-file", "", "Path to sector mapping JSON (same search chain as concentration)")
	cmd.Flags().StringVar(&jsonOut, "json", "", "Also write the full report as JSON to this path")
	cmd.Flags().StringVar(&analysisAddr, "analysis-addr", "localhost:50052", "Python analysis engine gRPC address")
	return cmd
}
