package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/internal/scanjournal"
	"github.com/IS908/optix/pkg/model"
	"github.com/spf13/cobra"
)

// yfDailyBars 生产 BarsSource：yfinance 日线（raw ticker，不走 AssetRef registry）。
type yfDailyBars struct{ pythonBin string }

func (y yfDailyBars) DailyBars(ctx context.Context, symbols []string, period string) (map[string][]model.OHLCV, error) {
	return marketdata.RawBatchBars(ctx, y.pythonBin, symbols, "1d", period)
}

func newScanJournalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan-journal",
		Short: "Sell-put scan journal — register candidates, reconcile expiries, band stats",
		Long: `可证伪扫描复盘：register 把每日 Top-N 候选入库（stdin JSON）；
reconcile 结算所有已到期未结算候选（hit/miss/void + P&L + touched，7 日历日宽限期）；
stats 按 score 分位/rank/dte 分档输出命中率。写路径只走本命令族（追加式，不可改写）。
设计: docs/superpowers/specs/2026-07-29-sellput-scan-journal-design.md`,
	}
	cmd.AddCommand(newScanJournalRegisterCmd(), newScanJournalReconcileCmd(), newScanJournalStatsCmd())
	return cmd
}

func scanJournalService() (*scanjournal.Service, *sqlite.Store, error) {
	store, err := sqlite.New(dbPath)
	if err != nil {
		return nil, nil, cliExit(fmt.Errorf("open database: %w", err), exitSQLiteErr)
	}
	RegisterCleanup(store)
	return scanjournal.NewService(store, yfDailyBars{pythonBin: pythonBin}), store, nil
}

func newScanJournalRegisterCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "register",
		Short: "Register today's scan candidates from stdin JSON (atomic, idempotent)",
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			var payload scanjournal.RegisterPayload
			if err := json.Unmarshal(raw, &payload); err != nil {
				return fmt.Errorf("parse payload JSON: %w", err)
			}
			svc, store, err := scanJournalService()
			if err != nil {
				return err
			}
			defer store.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			res, err := svc.Register(ctx, payload)
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(res)
		},
	}
}

func newScanJournalReconcileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Settle all expired unsettled candidates (idempotent; exit 0 always on data states)",
		RunE: func(_ *cobra.Command, _ []string) error {
			svc, store, err := scanJournalService()
			if err != nil {
				return err
			}
			defer store.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			res, err := svc.Reconcile(ctx)
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(res)
		},
	}
}

// validateScanJournalStatsFormat accepts "table" or "json" (case-insensitive).
//
// validateOutputFormat (maxpain.go) validates a different set ("text"|"json"),
// so scan-journal stats — whose default output is a table, not free text —
// gets its own two-value check rather than reusing that helper.
func validateScanJournalStatsFormat(s string) error {
	switch strings.ToLower(s) {
	case "table", "json":
		return nil
	}
	return fmt.Errorf("invalid --format %q: use table | json", s)
}

func newScanJournalStatsCmd() *cobra.Command {
	var window, by, format string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Banded hit-rate stats (score-band terciles by default)",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateScanJournalStatsFormat(format); err != nil {
				return err
			}
			svc, store, err := scanJournalService()
			if err != nil {
				return err
			}
			defer store.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			res, err := svc.Stats(ctx, window, by)
			if err != nil {
				return err
			}
			if strings.ToLower(format) == "json" {
				return json.NewEncoder(os.Stdout).Encode(res)
			}
			fmt.Printf("Scan journal stats  window=%s  by=%s\n", res.Window, res.By)
			if res.Note != "" {
				fmt.Println("note:", res.Note)
			}
			fmt.Println("band       n   hit%   avgP&L  touched%  avgBreach%")
			for _, b := range res.Bands {
				fmt.Printf("%-9s %3d  %5.1f  %+7.2f  %7.1f  %9.2f\n",
					b.Label, b.N, b.HitRate*100, b.AvgPnL, b.TouchedRate*100, b.AvgMaxBreach)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&window, "window", "all", "all|30d|90d")
	cmd.Flags().StringVar(&by, "by", "score-band", "score-band|rank|dte")
	cmd.Flags().StringVar(&format, "format", "table", "table|json")
	return cmd
}
