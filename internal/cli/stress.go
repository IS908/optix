package cli

import (
	"context"
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

func newPortfolioStressCmd() *cobra.Command {
	var (
		netLiqUSD    float64
		riskFreeRate float64
		sectorsFile  string
		configPath   string
		jsonOut      string
		analysisAddr string
	)
	cmd := &cobra.Command{
		Use:   "stress",
		Short: "Run scenario P&L stress tests from the portfolio Greeks snapshot",
		Long: `Run account-level scenario stress tests from the portfolio Greeks snapshot.

This first Phase 3 slice uses the same IBKR positions, option-chain IV, and
Black-Scholes Greeks path as 'portfolio greeks', then applies config-driven
SPY/QQQ/IV shocks to estimate per-scenario P&L.`,
		Example: `  optix portfolio stress --net-liq-usd 354477
  optix portfolio stress --portfolio-config configs/portfolio.yaml --json /tmp/stress.json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			analysisAddr = resolveAnalysisAddr(cmd, analysisAddr)
			cfg, resolvedRiskFreeRate, resolvedSectorsFile, err := resolveStressSettings(
				configPath, sectorsFile, riskFreeRate, cmd.Flags().Changed("risk-free-rate"),
			)
			if err != nil {
				return err
			}
			riskFreeRate = resolvedRiskFreeRate
			sectorsFile = resolvedSectorsFile

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
			if jsonOut == "-" {
				fmt.Fprintln(os.Stderr, b.SourceBanner())
			} else {
				fmt.Println(b.SourceBanner())
			}

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
			greeks, err := portfolio.AggregateGreeks(ctx, positions, portfolio.GreeksOptions{
				GroupBy: "underlying", NetLiqUSD: anchorNLV, RiskFreeRate: riskFreeRate, AsOf: time.Now().UTC(),
			}, pricer, market, sm)
			if err != nil {
				return cliExit(fmt.Errorf("aggregate greeks: %w", err), exitGenericErr)
			}

			betaProvider := buildStressBetaProvider(ctx, store, market, greeks.Groups, time.Now().UTC(), os.Stderr)
			report := portfolio.RunStressWithRepricing(ctx, greeks, cfg.Stress.Scenarios, betaProvider, pricer)
			if jsonOut == "-" {
				return writeJSONDestination(os.Stdout, jsonOut, report)
			}
			portfolio.RenderStress(report, os.Stdout)

			if jsonOut != "" {
				if err := writeJSONDestination(os.Stdout, jsonOut, report); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "\nJSON snapshot written to %s\n", jsonOut)
			}
			return nil
		},
	}
	cmd.Flags().Float64Var(&netLiqUSD, "net-liq-usd", 0, "Net Liq value in USD (anchors stress % NLV). Omit to use sum(|MV|) fallback.")
	cmd.Flags().Float64Var(&riskFreeRate, "risk-free-rate", 0, "Risk-free rate for Black-Scholes (default from config, then 0.043)")
	cmd.Flags().StringVar(&sectorsFile, "sectors-file", "", "Path to sector mapping JSON (defaults to config/search chain)")
	cmd.Flags().StringVar(&configPath, "portfolio-config", "configs/portfolio.yaml", "Path to portfolio risk YAML config; missing file uses defaults")
	cmd.Flags().StringVar(&jsonOut, "json", "", "Also write the full report as JSON to this path, or '-' for stdout only")
	cmd.Flags().StringVar(&analysisAddr, "analysis-addr", defaultAnalysisAddr, "Python analysis engine gRPC address")
	return cmd
}

func resolveStressSettings(configPath, sectorsFile string, riskFreeRate float64, riskFreeRateSet bool) (portfolio.PortfolioConfig, float64, string, error) {
	cfg, err := portfolio.LoadConfig(configPath)
	if err != nil {
		return portfolio.PortfolioConfig{}, 0, "", err
	}
	if !riskFreeRateSet {
		riskFreeRate = cfg.Greeks.RiskFreeRate
	}
	if sectorsFile == "" {
		sectorsFile = cfg.SectorsFile
	}
	return cfg, riskFreeRate, sectorsFile, nil
}
