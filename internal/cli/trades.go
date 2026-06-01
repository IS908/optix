package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/server"
	"github.com/IS908/optix/pkg/model"
	"github.com/spf13/cobra"
)

// tradesClientID is the IBKR ClientID used by `optix trades`. Like
// journalClientID, it must be 0 (IBKR implicit master) to get cross-client
// execution visibility from `reqExecutions`. A non-zero ClientID would only
// see fills the optix process itself placed — but optix is read-only and
// never places orders, so a non-zero ID effectively returns 0 fills. See
// issue #30.
const tradesClientID = 0

func newTradesCmd() *cobra.Command {
	var symbol, side, since string

	cmd := &cobra.Command{
		Use:   "trades",
		Short: "Show executions from the last 7 days",
		Long: `Show the account's execution history from IBKR.

IBKR's execution API only returns the last ~7 days. --since can narrow the
window but cannot reach further back; a --since older than 7 days is clamped
with a warning.

Requires IBKR TWS/Gateway — account data is not available via Yahoo Finance.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			if side != "" {
				normalized := strings.ToUpper(side)
				if normalized != "BOT" && normalized != "SLD" {
					// Preserve the user's original input in the error so they can
					// spot typos at a glance (don't echo back the uppercased form).
					return fmt.Errorf("invalid --side %q: use BOT or SLD", side)
				}
				side = normalized
			}
			symbol = strings.ToUpper(symbol)

			filter := model.ExecutionFilter{Symbol: symbol, Side: side}
			if since != "" {
				t, err := time.Parse("2006-01-02", since)
				if err != nil {
					return fmt.Errorf("invalid --since %q: use YYYY-MM-DD", since)
				}
				clamped, wasClamped := clampSinceTo7Days(t, time.Now())
				if wasClamped {
					fmt.Printf("⚠️  --since %s is older than 7 days; IBKR only returns the last 7 days — clamping to %s\n",
						since, clamped.Format("2006-01-02"))
				}
				filter.Since = clamped
			}

			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(fmt.Errorf("open database: %w", err), exitSQLiteErr)
			}
			RegisterCleanup(store)
			defer store.Close()

			b := factory.NewWithFallback(ibkr.Config{
				Host:     ibHost,
				Port:     ibPort,
				ClientID: tradesClientID,
			}, pythonBin)
			if err := b.Connect(ctx); err != nil {
				return cliExit(fmt.Errorf("connect to broker: %w", err), exitIBKRUnreachable)
			}
			defer b.Disconnect()
			fmt.Println(b.SourceBanner())

			market := server.NewMarketDataService(b, store)
			acct := server.NewAccountService(b, market)

			execs, err := acct.GetExecutions(ctx, filter)
			if err != nil {
				if errors.Is(err, broker.ErrAccountNotSupported) {
					return cliExit(fmt.Errorf("账户数据需要 IBKR 连接，当前已回退到 Yahoo Finance（无账户接口）。请确认 TWS/Gateway 在运行"), exitIBKRUnreachable)
				}
				return cliExit(fmt.Errorf("get executions: %w", err), exitIBKRUnreachable)
			}

			if len(execs) == 0 {
				fmt.Println("No executions in the last 7 days.")
				return nil
			}

			fmt.Printf("%-10s %-18s %-8s %-5s %8s %10s %-10s %10s\n",
				"Account", "Time", "Symbol", "Side", "Qty", "Price", "Exchange", "OrderID")
			for _, e := range execs {
				ts := "—"
				if !e.Time.IsZero() {
					ts = e.Time.Format("2006-01-02 15:04")
				}
				fmt.Printf("%-10s %-18s %-8s %-5s %8.0f %10.2f %-10s %10d\n",
					e.Account, ts, e.Symbol, e.Side, e.Shares, e.Price, e.Exchange, e.OrderID)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&symbol, "symbol", "", "Filter by symbol")
	cmd.Flags().StringVar(&side, "side", "", "Filter by side: BOT | SLD")
	cmd.Flags().StringVar(&since, "since", "", "Only executions on/after this date (YYYY-MM-DD, within last 7 days)")
	return cmd
}

// clampSinceTo7Days bounds t to IBKR's 7-day ReqExecutions window relative to
// now. The boundary is date-aligned at UTC midnight so a --since matching
// today minus 7 calendar days passes through (no spurious clamp from a
// wall-clock time-of-day component). Returns the clamped time and a flag
// indicating whether clamping happened, so the caller can emit a warning.
func clampSinceTo7Days(t, now time.Time) (time.Time, bool) {
	sevenDaysAgo := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -7)
	if t.Before(sevenDaysAgo) {
		return sevenDaysAgo, true
	}
	return t, false
}
