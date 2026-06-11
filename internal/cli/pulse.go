package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/spf13/cobra"
)

// nyLoc 加载一次；America/New_York 决定时段推断（含 DST）。
var nyLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.FixedZone("EST", -5*3600) // 极端环境兜底
	}
	return loc
}()

// inferView 是纯时钟映射（NOT M2 状态机）：
// 04:00-09:30 premarket · 09:30-16:00 intraday · 16:00-20:00 postclose · 其余(隔夜)→premarket。
// event/shock 仅显式 --view 可达。
func inferView(now time.Time) marketdata.View {
	t := now.In(nyLoc)
	mins := t.Hour()*60 + t.Minute()
	switch {
	case mins >= 4*60 && mins < 9*60+30:
		return marketdata.ViewPremarket
	case mins >= 9*60+30 && mins < 16*60:
		return marketdata.ViewIntraday
	case mins >= 16*60 && mins < 20*60:
		return marketdata.ViewPostclose
	default: // 20:00-04:00 隔夜 → 看隔夜期货
		return marketdata.ViewPremarket
	}
}

// pulse JSON 契约（spec §6）。Price 用指针：pct-only 代理资产输出 null。
type pulseAssetJSON struct {
	ID        string    `json:"id"`
	Class     string    `json:"class"`
	Label     string    `json:"label"`
	Price     *float64  `json:"price"`
	Change    float64   `json:"change"`
	ChangePct float64   `json:"change_pct"`
	Basis     string    `json:"basis"`
	AsOf      time.Time `json:"as_of"`
	Spark     []float64 `json:"spark,omitempty"`
	SparkWin  string    `json:"spark_window,omitempty"`
}

type pulseJSON struct {
	SnapshotAt   time.Time        `json:"snapshot_at"`
	View         string           `json:"view"`
	ViewInferred bool             `json:"view_inferred"`
	Assets       []pulseAssetJSON `json:"assets"`
	Missing      []string         `json:"missing,omitempty"`
	Warnings     []string         `json:"warnings,omitempty"`
}

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

The view defaults to a pure clock mapping (America/New_York): premarket /
intraday / postclose; overnight maps to premarket (overnight futures).
event/shock views are reachable only via --view. Data basis (delayed /
approx / frozen) is labeled per asset; assets that fail to fetch are
listed in missing[] without failing the whole snapshot.`,
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
				view = inferView(time.Now())
				inferred = true
			} else if !isValidView(view) {
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

			router := marketdata.NewRouter()
			yf := marketdata.NewYFinanceSource(pythonBin)
			for _, c := range []marketdata.AssetClass{
				marketdata.ClassIndex, marketdata.ClassFuture, marketdata.ClassStock,
				marketdata.ClassFX, marketdata.ClassYield, marketdata.ClassVol,
			} {
				router.Register(c, yf)
			}
			svc := marketdata.NewPulseService(router, store)

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
				return writePulseJSON(snap, inferred)
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

func isValidView(v marketdata.View) bool {
	for _, ok := range marketdata.ValidViews {
		if v == ok {
			return true
		}
	}
	return false
}

func writePulseJSON(snap *marketdata.PulseSnapshot, inferred bool) error {
	out := pulseJSON{
		SnapshotAt: snap.SnapshotAt, View: string(snap.View), ViewInferred: inferred,
		Assets:  make([]pulseAssetJSON, 0, len(snap.Assets)),
		Missing: snap.Missing, Warnings: snap.Warnings,
	}
	for _, a := range snap.Assets {
		pj := pulseAssetJSON{
			ID: a.Ref.ID, Class: string(a.Ref.Class), Label: a.Label,
			Change: a.Change, ChangePct: a.ChangePct,
			Basis: string(a.Basis), AsOf: a.AsOf,
			Spark: a.Spark, SparkWin: a.SparkWindow,
		}
		if !a.PctOnly {
			p := a.Price
			pj.Price = &p
		}
		out.Assets = append(out.Assets, pj)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func renderPulseText(snap *marketdata.PulseSnapshot, inferred bool) {
	mode := "explicit"
	if inferred {
		mode = "inferred"
	}
	fmt.Printf("═══ MARKET PULSE (%s · %s) ═══  %s\n\n",
		snap.View, mode, snap.SnapshotAt.In(nyLoc).Format("2006-01-02 15:04 MST"))
	fmt.Printf("%-10s %12s %8s   %-8s %s\n", "ID", "Price", "Chg%", "Basis", "As-of")
	for _, a := range snap.Assets {
		price := "—"
		if !a.PctOnly {
			price = fmt.Sprintf("%.2f", a.Price)
		}
		fmt.Printf("%-10s %12s %+7.2f%%   %-8s %s\n",
			a.Ref.ID, price, a.ChangePct, a.Basis, a.AsOf.In(nyLoc).Format("15:04"))
	}
	for _, id := range snap.Missing {
		fmt.Printf("%-10s %12s %8s   %-8s\n", id, "—", "—", "missing")
	}
	// proxy note: one line per pct-only asset (Price unavailable, ChangePct only).
	for _, a := range snap.Assets {
		if a.PctOnly {
			fmt.Printf("note: %s = %s, 涨跌幅代理, 无点位\n", a.Ref.ID, a.Label)
		}
	}
	if len(snap.Warnings) > 0 {
		fmt.Println()
		for _, w := range snap.Warnings {
			fmt.Printf("⚠ %s\n", w)
		}
	}
}
