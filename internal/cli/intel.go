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

// newIntelJournal 打开 store + 构建 IntelJournal（judge 用 yfinance 路由抓登记价）。
// 返回的 closer 须 defer。
func newIntelJournal() (*intel.IntelJournal, func(), error) {
	store, err := sqlite.New(dbPath)
	if err != nil {
		return nil, nil, cliExit(fmt.Errorf("open database: %w", err), exitSQLiteErr)
	}
	RegisterCleanup(store)
	price := marketdata.NewPulseService(marketdata.NewYFinanceRouter(pythonBin), store)
	return intel.NewIntelJournal(store, price), func() { store.Close() }, nil
}

func newIntelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "intel",
		Short: "Market Intel judgment journal — checkpoint narratives, judgments, reconciliation",
		Long: `The Market Intel judgment journal: at each daily checkpoint (08:00 剧本 /
10:30 首验 / 15:00 定调 / 16:30 对账) an agent writes narrative prose and registers
falsifiable directional judgments; optix captures the registration price, settles
expired judgments against market price history (reconcile), and tracks hit-rate.

No IBKR and no Python gRPC engine required (judge uses the skill venv's yfinance to
capture the registration price; everything else is local SQLite).`,
	}
	cmd.AddCommand(newIntelStatusCmd(), newIntelReadCmd(), newIntelNarrativeCmd(),
		newIntelJudgeCmd(), newIntelReconcileCmd())
	return cmd
}

func newIntelStatusCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Current phase, checkpoint schedule status, today's hit-rate",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			j, closer, err := newIntelJournal()
			if err != nil {
				return err
			}
			defer closer()
			snap, err := j.Status(context.Background())
			if err != nil {
				return cliExit(fmt.Errorf("intel status: %w", err), exitGenericErr)
			}
			return emitJSONOrText(format, snap, func() { renderIntelStatus(snap) })
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

func newIntelReadCmd() *cobra.Command {
	var format, date string
	cmd := &cobra.Command{
		Use:   "read",
		Short: "Read a trading day's narratives, judgments, and reconciliations",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			j, closer, err := newIntelJournal()
			if err != nil {
				return err
			}
			defer closer()
			if date == "" {
				date = intel.TradingDate(time.Now())
			}
			snap, err := j.ReadJournal(context.Background(), date)
			if err != nil {
				return cliExit(fmt.Errorf("intel read: %w", err), exitGenericErr)
			}
			return emitJSONOrText(format, snap, func() { renderIntelJournal(snap) })
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	cmd.Flags().StringVar(&date, "date", "", "Trading date YYYY-MM-DD (default: today ET)")
	return cmd
}

func newIntelNarrativeCmd() *cobra.Command {
	var format, kind, body string
	cmd := &cobra.Command{
		Use:   "narrative",
		Short: "Write a checkpoint narrative entry (append-only)",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			if kind == "" || body == "" {
				return cliExit(fmt.Errorf("--kind and --body are required"), exitGenericErr)
			}
			j, closer, err := newIntelJournal()
			if err != nil {
				return err
			}
			defer closer()
			n, err := j.WriteNarrative(context.Background(), kind, body)
			if err != nil {
				return cliExit(fmt.Errorf("write narrative: %w", err), exitGenericErr)
			}
			return emitJSONOrText(format, n, func() {
				fmt.Printf("✓ narrative written: %s [%s] %s\n", n.EntryID, n.Checkpoint, n.TradingDate)
			})
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	cmd.Flags().StringVar(&kind, "kind", "", "Checkpoint: script|first_check|set_tone|reconcile|interrupt")
	cmd.Flags().StringVar(&body, "body", "", "Narrative prose")
	return cmd
}

func newIntelJudgeCmd() *cobra.Command {
	var format, asset, direction, expiry, rationale, supersedes string
	var threshold float64
	var confidence int
	cmd := &cobra.Command{
		Use:   "judge",
		Short: "Register a falsifiable directional judgment (optix captures the price)",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			j, closer, err := newIntelJournal()
			if err != nil {
				return err
			}
			defer closer()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			jd, err := j.RegisterJudgment(ctx, intel.JudgmentInput{
				AssetID: asset, Direction: direction, ThresholdPct: threshold,
				Confidence: confidence, ExpiryCheckpoint: expiry,
				Rationale: rationale, Supersedes: supersedes,
			})
			if err != nil {
				return cliExit(fmt.Errorf("register judgment: %w", err), exitGenericErr)
			}
			return emitJSONOrText(format, jd, func() {
				fmt.Printf("✓ judgment %s: %s %s (th %.2f%%, conf %d) → %s @ %.2f\n",
					jd.JudgmentID, jd.AssetID, jd.Direction, jd.ThresholdPct, jd.Confidence,
					jd.ExpiryCheckpoint, jd.RegisteredPrice)
			})
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	cmd.Flags().StringVar(&asset, "asset", "", "Asset business ID (SPX/ES/VIX/...)")
	cmd.Flags().StringVar(&direction, "direction", "", "up | down | flat")
	cmd.Flags().Float64Var(&threshold, "threshold", 0, "Threshold %% (up/down trigger line; flat tolerance band)")
	cmd.Flags().IntVar(&confidence, "confidence", 50, "Confidence 0-100")
	cmd.Flags().StringVar(&expiry, "expiry", "", "Expiry checkpoint: first_check|set_tone|reconcile")
	cmd.Flags().StringVar(&rationale, "rationale", "", "Optional short rationale")
	cmd.Flags().StringVar(&supersedes, "supersedes", "", "Optional judgment_id this supersedes")
	return cmd
}

func newIntelReconcileCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Settle expired judgments against price history (the 对账 step)",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			j, closer, err := newIntelJournal()
			if err != nil {
				return err
			}
			defer closer()
			res, err := j.Reconcile(context.Background())
			if err != nil {
				return cliExit(fmt.Errorf("reconcile: %w", err), exitGenericErr)
			}
			return emitJSONOrText(format, res, func() {
				fmt.Printf("✓ reconciled %d judgment(s)\n", res.Settled)
			})
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

// emitJSONOrText：format=json 时 indent 输出 v；否则跑 textFn。
func emitJSONOrText(format string, v any, textFn func()) error {
	if strings.ToLower(format) == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	textFn()
	return nil
}

func renderIntelStatus(s intel.StatusSnapshot) {
	fmt.Printf("═══ INTEL STATUS  %s · %s ═══\n", s.Now.Format("2006-01-02 15:04 MST"), s.Phase)
	for _, c := range s.Checkpoints {
		fmt.Printf("  %-12s %-6s %s\n", c.Label, c.Status, c.DueAt.Format("15:04"))
	}
	fmt.Printf("today: %d judgment(s), hit-rate %d/%d", s.JudgmentCount, s.HitRate.Hit, s.HitRate.Hit+s.HitRate.Miss)
	if s.HitRate.Hit+s.HitRate.Miss > 0 {
		fmt.Printf(" (%.0f%%)", s.HitRate.Rate*100)
	}
	fmt.Println()
}

func renderIntelJournal(s intel.JournalSnapshot) {
	fmt.Printf("═══ INTEL JOURNAL  %s ═══\n", s.TradingDate)
	for _, n := range s.Narratives {
		fmt.Printf("\n[%s · %s]\n%s\n", n.Checkpoint, n.Phase, n.Body)
	}
	if len(s.Judgments) > 0 {
		fmt.Println("\njudgments:")
		for _, j := range s.Judgments {
			status := "⏳pending"
			if j.Reconciliation != nil {
				status = j.Reconciliation.Outcome
			}
			fmt.Printf("  %-8s %-4s th%.2f%% conf%d → %-11s %s\n",
				j.AssetID, j.Direction, j.ThresholdPct, j.Confidence, j.ExpiryCheckpoint, status)
		}
	}
	fmt.Printf("\nhit-rate %d/%d\n", s.HitRate.Hit, s.HitRate.Hit+s.HitRate.Miss)
}
