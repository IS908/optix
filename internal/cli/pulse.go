package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/intel"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/spf13/cobra"
)

func newPulseCmd() *cobra.Command {
	var (
		viewFlag  string
		format    string
		withSpark bool
		strict    bool
	)
	cmd := &cobra.Command{
		Use:   "pulse",
		Short: "Market pulse snapshot (multi-asset, per-view) — no IBKR required",
		Long: `Multi-asset market snapshot for the Market Intel views: indices, futures
proxies, FX, yields and vol family via free delayed sources (yfinance).

The view defaults to the shared market phase clock (America/New_York):
premarket / intraday / postclose. Outside trading hours (overnight /
weekends / NYSE holidays) the view maps to postclose — the last session's
frozen snapshot. event/shock views are reachable only via --view (this
CLI's auto-view is intentionally narrower than the HTTP /api/intel/pulse
endpoint, which also auto-promotes to event/shock on FOMC/CPI days and
shock regimes). Data
basis (delayed / approx / frozen), source and basis_note are labeled per
asset. M1 intentionally keeps FRED/CBOE official historical feeds out of the
30s Pulse loop; yield and proxy rows say when they are yfinance approximations.
Assets that fail to fetch are listed in missing[] without failing the whole
snapshot.`,
		Example: `  optix pulse
  optix pulse --view premarket --format json
  optix pulse --format json --with-sparkline`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			format = strings.ToLower(format)

			view := marketdata.View(viewFlag)
			inferred := false
			if viewFlag == "" {
				// CLI auto-view: phase clock only — premarket / intraday / postclose.
				// Deliberately ASYMMETRIC with HTTP handlePulse, which also auto-promotes
				// to event/shock via resolveAutoView. Reaching event/shock from the CLI
				// requires --view explicitly; same schema, different inference. The Long
				// help above states this, and CLAUDE.md `handlers.go` description spells
				// out the divergence so JSON consumers don't expect a portable `view`
				// field across the two surfaces when view_inferred=true. (#163)
				view = intel.ViewFor(intel.PhaseAt(time.Now()))
				inferred = true
			} else if !intel.ValidView(view) {
				return fmt.Errorf("invalid --view %q: use premarket|intraday|postclose|event|shock", viewFlag)
			}

			// 子进程取数（yfinance）整体受超时约束，避免 hang 住命令；
			// 2 分钟与 dashboard 的批量分析超时（120s）对齐。
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(fmt.Errorf("open database: %w", err), exitSQLiteErr)
			}
			RegisterCleanup(store)
			defer store.Close()

			// 机会式清理：独立运行的 optix pulse 不经过 server 调度器，
			// 在这里 best-effort 强制 2 天滚动窗口；失败仅告警，不影响命令。
			if _, err := store.PrunePulseBars(ctx, sqlite.PulseBarRetention); err != nil {
				fmt.Fprintf(os.Stderr, "warning: prune pulse bars: %v\n", err)
			}

			router := marketdata.NewYFinanceRouter(pythonBin)
			svc := marketdata.NewPulseService(router, store)

			// --with-sparkline is JSON-only; skip the bars subprocess in text mode.
			withSpark = withSpark && format == "json"
			snap, err := svc.Snapshot(ctx, view, withSpark)
			if err != nil {
				return cliExit(fmt.Errorf("pulse snapshot: %w", err), exitGenericErr)
			}
			// snap 是缓存共享对象（指针）—— 只读，不得在渲染/JSON 路径修改。
			if strict && len(snap.Assets) == 0 {
				msg := "no assets available"
				if len(snap.Warnings) > 0 {
					msg += ": " + strings.Join(snap.Warnings, "; ")
				}
				return cliExit(fmt.Errorf("%s", msg), exitGenericErr)
			}

			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(intel.ToPulseDTO(snap, inferred))
			}
			renderPulseText(snap, inferred)
			return nil
		},
	}
	cmd.Flags().StringVar(&viewFlag, "view", "", "View: premarket|intraday|postclose|event|shock (default: clock-inferred)")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	cmd.Flags().BoolVar(&withSpark, "with-sparkline", false, "Fetch 5m sparkline bars and include spark[] in JSON output (JSON only; ignored by text renderer)")
	cmd.Flags().BoolVar(&strict, "strict", false, "Exit non-zero when no asset data is available")
	return cmd
}

func renderPulseText(snap *marketdata.PulseSnapshot, inferred bool) {
	mode := "explicit"
	if inferred {
		mode = "inferred"
	}
	fmt.Printf("═══ MARKET PULSE (%s · %s) ═══  %s\n\n",
		snap.View, mode, snap.SnapshotAt.In(intel.NY()).Format("2006-01-02 15:04 MST"))
	fmt.Printf("%-10s %12s %8s   %-8s %-10s %s\n", "ID", "Price", "Chg%", "Basis", "Source", "As-of")
	for _, a := range snap.Assets {
		price := "—"
		if !a.PctOnly {
			price = fmt.Sprintf("%.2f", a.Price)
		}
		fmt.Printf("%-10s %12s %+7.2f%%   %-8s %-10s %s\n",
			a.Ref.ID, price, a.ChangePct, a.Basis, a.Source, a.AsOf.In(intel.NY()).Format("15:04"))
	}
	for _, id := range snap.Missing {
		fmt.Printf("%-10s %12s %8s   %-8s\n", id, "—", "—", "missing")
	}
	// Source notes are intentionally explicit for approx/frozen assets.
	for _, a := range snap.Assets {
		if a.PctOnly || a.Basis == marketdata.BasisApprox || a.Basis == marketdata.BasisFrozen {
			fmt.Printf("source: %s = %s\n", a.Ref.ID, marketdata.BasisNote(a.Quote))
		}
	}
	if len(snap.Warnings) > 0 {
		fmt.Println()
		for _, w := range snap.Warnings {
			fmt.Printf("⚠ %s\n", w)
		}
	}
}
