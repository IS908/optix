package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/eventintel"
	"github.com/IS908/optix/internal/intel"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/internal/postclose"
	"github.com/IS908/optix/internal/premarket"
	"github.com/IS908/optix/internal/scheduler"
	"github.com/IS908/optix/internal/server"
	"github.com/IS908/optix/internal/shockintel"
	"github.com/IS908/optix/internal/webui"
	"github.com/IS908/optix/pkg/model"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// journalBrokerAdapter satisfies the accountReader interface required by
// server.NewJournalService. Each call opens a fresh broker connection
// (journalClientID, matching cli/journal.go), reads executions, and disconnects.
// This keeps the web UI from holding an idle IBKR slot between syncs.
type journalBrokerAdapter struct {
	host      string
	port      int
	pythonBin string
}

func (a *journalBrokerAdapter) GetExecutions(ctx context.Context, filter model.ExecutionFilter) ([]model.Execution, error) {
	b := factory.NewWithFallback(ibkr.Config{
		Host: a.host, Port: a.port, ClientID: journalClientID,
	}, a.pythonBin)
	if err := b.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect to broker: %w", err)
	}
	defer b.Disconnect()
	return b.GetExecutions(ctx, filter)
}

// runJournalSyncTicker runs an initial catch-up sync at startup and then
// fires on the given interval. Failures are logged (and recorded in
// sync_state.last_error via SyncExecutions) but never block the listener.
func runJournalSyncTicker(ctx context.Context, svc *server.JournalService, interval time.Duration) {
	if n, err := svc.SyncExecutions(ctx); err != nil {
		log.Warn().Err(err).Msg("journal: initial sync failed")
		fmt.Fprintf(os.Stderr, "journal: initial sync failed: %v\n", err)
	} else {
		log.Info().Int("new", n).Msg("journal: initial sync ok")
		fmt.Fprintf(os.Stderr, "journal: initial sync ok (%d new)\n", n)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := svc.SyncExecutions(ctx); err != nil {
				log.Warn().Err(err).Msg("journal: scheduled sync failed")
			} else {
				log.Info().Int("new", n).Msg("journal: scheduled sync ok")
			}
		}
	}
}

func newServerCmd() *cobra.Command {
	var (
		webAddr             string
		analysisAddr        string
		capital             float64
		forecastDays        int
		riskTol             string
		maxBrokers          int
		journalSyncInterval time.Duration
	)

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start the Optix web UI server",
		Long: `Start the Optix lightweight web UI server.

The server serves an HTML dashboard and per-symbol analysis pages backed by a
SQLite cache (default) or live IB Gateway/TWS + Python analysis engine (?refresh=true).

Examples:
  optix server
  optix server --web-addr=0.0.0.0:8080
  optix server --analysis-addr=localhost:50052 --capital=100000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			analysisAddr = resolveAnalysisAddr(cmd, analysisAddr)
			// 1. Open SQLite store
			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(fmt.Errorf("open database: %w", err), exitSQLiteErr)
			}
			RegisterCleanup(store)
			defer store.Close()

			// 2. Build web UI config
			cfg := webui.Config{
				Addr:                 webAddr,
				IBHost:               ibHost,
				IBPort:               ibPort,
				AnalysisAddr:         analysisAddr,
				Capital:              capital,
				ForecastDays:         int32(forecastDays),
				RiskTolerance:        riskTol,
				PythonBin:            pythonBin,
				MaxConcurrentBrokers: maxBrokers,
			}

			// 3. Build JournalService (broker connection opened lazily per sync)
			journalSvc := server.NewJournalService(
				&journalBrokerAdapter{host: ibHost, port: ibPort, pythonBin: pythonBin},
				store,
			)

			// 4. Create server with journal endpoints wired in
			srv := webui.NewWithJournal(cfg, store, &webui.JournalDeps{
				Service: journalSvc,
				Store:   store,
			})

			// 4b. Market Intel API：phase 状态机 + pulse 快照 + judgment journal（零 IBKR，yfinance 路由）
			// HTTP pulse 路径跑在 r.Context() 下，受服务器 120s WriteTimeout 约束，无需 CLI 式 2 分钟 ctx。
			intelPulse := marketdata.NewPulseService(marketdata.NewYFinanceRouter(pythonBin), store)
			postcloseSvc, err := postclose.NewDefaultService(pythonBin)
			if err != nil {
				return fmt.Errorf("postclose service: %w", err)
			}
			srv.AttachIntel(&intel.Handlers{
				Pulse:     intelPulse,
				Journal:   intel.NewIntelJournal(store, intelPulse),
				Premarket: premarket.NewService(premarket.NewMarketAdapter(pythonBin), store),
				Postclose: postcloseSvc,
				Event:     eventintel.NewDefaultService(pythonBin),
				Shock:     shockintel.NewIBKRPreferredService(ibHost, ibPort, pythonBin),
				Watchlist: func(ctx context.Context) ([]string, error) {
					items, err := store.GetWatchlist(ctx)
					return watchlistSymbols(items), err
				},
			})

			// 5. Initialize and start background scheduler
			schedulerCfg := scheduler.Config{
				WorkerCount:    5,
				QueueSize:      100,
				TickInterval:   1 * time.Minute,
				WorkerThrottle: 12 * time.Second,
			}

			sched := scheduler.New(
				schedulerCfg,
				store,
				scheduler.IBConfig{Host: ibHost, Port: ibPort, PythonBin: pythonBin},
				scheduler.AnalysisConfig{
					Addr:          analysisAddr,
					Capital:       capital,
					ForecastDays:  int32(forecastDays),
					RiskTolerance: riskTol,
				},
			)

			// 6. Listen for OS signals → cancel context for graceful shutdown
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Start scheduler in the background
			if err := sched.Start(ctx); err != nil {
				return fmt.Errorf("start scheduler: %w", err)
			}
			log.Info().
				Int("workers", schedulerCfg.WorkerCount).
				Dur("tick_interval", schedulerCfg.TickInterval).
				Msg("Background scheduler started")

			// 7. Start journal sync ticker (catch-up sync + periodic refresh)
			if journalSyncInterval > 0 {
				go runJournalSyncTicker(ctx, journalSvc, journalSyncInterval)
				log.Info().
					Dur("interval", journalSyncInterval).
					Msg("Journal sync ticker started")
			}

			fmt.Printf("%-17s %s:%d\n", serverBrokerLabel(), ibHost, ibPort)
			fmt.Printf("Analysis engine:  %s\n", analysisAddr)
			fmt.Printf("Database:         %s\n", dbPath)
			fmt.Printf("Capital:          $%.0f\n", capital)

			// 8. Start serving (blocks until ctx cancelled or fatal error)
			return srv.Start(ctx)
		},
	}

	cmd.Flags().StringVar(&webAddr, "web-addr", "127.0.0.1:8080", "HTTP listen address")
	cmd.Flags().StringVar(&analysisAddr, "analysis-addr", defaultAnalysisAddr, "Python analysis engine gRPC address")
	cmd.Flags().Float64Var(&capital, "capital", 100000, "Available capital for strategy sizing")
	cmd.Flags().IntVar(&forecastDays, "forecast-days", 14, "Forecast horizon in days")
	cmd.Flags().StringVar(&riskTol, "risk-tolerance", "moderate", "Risk tolerance: conservative, moderate, aggressive")
	cmd.Flags().IntVar(&maxBrokers, "max-brokers", 0, "Max concurrent IBKR connections (0 = default 8; keep < 32 minus CLI/scheduler slots)")
	cmd.Flags().DurationVar(&journalSyncInterval, "journal-sync-interval", 6*time.Hour, "Background trade-journal sync interval; 0 disables")

	return cmd
}

func serverBrokerLabel() string {
	return "IB Gateway/TWS:"
}
