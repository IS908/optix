package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/webui"
	"github.com/spf13/cobra"
)

func newServerCmd() *cobra.Command {
	var (
		webAddr      string
		analysisAddr string
		capital      float64
		forecastDays int
		riskTol      string
	)

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start the Optix web UI server",
		Long: `Start the Optix lightweight web UI server.

The server serves an HTML dashboard and per-symbol analysis pages backed by a
SQLite cache (default) or live IB TWS + Python analysis engine (?refresh=true).

Examples:
  optix server
  optix server --web-addr=0.0.0.0:8080
  optix server --analysis-addr=localhost:50052 --capital=100000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Open SQLite store
			store, err := sqlite.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer store.Close()

			// 2. Build web UI config
			cfg := webui.Config{
				Addr:          webAddr,
				IBHost:        ibHost,
				IBPort:        ibPort,
				AnalysisAddr:  analysisAddr,
				Capital:       capital,
				ForecastDays:  int32(forecastDays),
				RiskTolerance: riskTol,
			}

			// 3. Create server
			srv := webui.New(cfg, store)

			// 4. Listen for OS signals → cancel context for graceful shutdown
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			fmt.Printf("IB TWS:           %s:%d\n", ibHost, ibPort)
			fmt.Printf("Analysis engine:  %s\n", analysisAddr)
			fmt.Printf("Database:         %s\n", dbPath)
			fmt.Printf("Capital:          $%.0f\n", capital)

			// 5. Start serving (blocks until ctx cancelled or fatal error)
			return srv.Start(ctx)
		},
	}

	cmd.Flags().StringVar(&webAddr, "web-addr", "127.0.0.1:8080", "HTTP listen address")
	cmd.Flags().StringVar(&analysisAddr, "analysis-addr", "localhost:50052", "Python analysis engine gRPC address")
	cmd.Flags().Float64Var(&capital, "capital", 100000, "Available capital for strategy sizing")
	cmd.Flags().IntVar(&forecastDays, "forecast-days", 14, "Forecast horizon in days")
	cmd.Flags().StringVar(&riskTol, "risk-tolerance", "moderate", "Risk tolerance: conservative, moderate, aggressive")

	return cmd
}
