package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/server"
	"github.com/IS908/optix/pkg/model"
	"github.com/spf13/cobra"
)

// ExitErr translates a journal-command failure into a documented exit code.
type ExitErr struct {
	Err  error
	Code int
}

func (e *ExitErr) Error() string { return e.Err.Error() }
func (e *ExitErr) Unwrap() error { return e.Err }

const (
	exitOK              = 0
	exitGenericErr      = 1
	exitIBKRUnreachable = 2
	exitSQLiteErr       = 3
	// NOTE: exit code 4 ("partial sync") was removed — UpsertExecutions is
	// transactional (all-or-nothing), so a partial-write state is unreachable.
	// Reserved; do not re-use without revisiting docs/journal_json_schema.md
	// and skills/commands/optix/SKILL.md.
	exitNoData = 5 // the broker/gateway was reachable and responded, but the
	// specific request itself had no usable result — e.g. `option-quote`
	// hitting IB error 200 "no security definition" for a bad
	// strike/expiry/right, or an empty/degraded quote after the collection
	// window. Distinct from exitIBKRUnreachable (2), which means the
	// gateway/connection itself failed, so scanner-style callers can branch
	// on exit code alone instead of string-matching stderr (#193 finding 6a).

	// journalClientID is the IBKR ClientID used by every `optix journal …`
	// subcommand and by the web UI's background journal sync ticker.
	//
	// IBKR's `reqExecutions` only returns the connecting client's own fills
	// unless the connecting ClientID matches TWS's "Master API Client ID"
	// setting. Since optix is read-only (never places orders), a non-zero
	// ClientID would always see 0 fills. ClientID 0 is IBKR's implicit
	// master — it gets cross-client execution visibility without requiring
	// users to configure anything in TWS. See issue #30.
	//
	// Users who have explicitly configured TWS → API → Master API Client ID
	// to a different non-zero value will need to match that value here (or
	// clear the TWS setting). No CLI override exists today; if this becomes
	// a real need, expose it as a flag.
	//
	// Other CLI subcommands use their own non-zero IDs because they only
	// need to read account/market data (positions, quotes) that does not
	// require master client status: 1 quote, 2 analyze single, 3 dashboard,
	// 4 positions, 5 portfolio, 6 `analyze --watchlist`, 8 max-pain, 9 chain.
	// Scheduler workers start at 10 and the web pool starts at 30. `trades`
	// reads executions like journal, so it also uses ClientID 0 (see tradesClientID).
	journalClientID = 0
)

func cliExit(err error, code int) error {
	if err == nil {
		return nil
	}
	return &ExitErr{Err: err, Code: code}
}

// AsExitCode unwraps an ExitErr returned by journal commands. Returns 0 for
// nil errors, the documented code for ExitErr, or 1 for any other error.
func AsExitCode(err error) int {
	var ex *ExitErr
	if errors.As(err, &ex) {
		return ex.Code
	}
	if err == nil {
		return exitOK
	}
	return exitGenericErr
}

func newJournalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "journal",
		Short: "Trade journal: persist IBKR executions and review trades",
		Long: `Persist IBKR executions to SQLite (works around IBKR's 7-day
history limit) and inspect them as round trips and review summaries.

This skill is READ-ONLY with respect to IBKR — it never places, modifies,
or cancels orders.`,
	}
	cmd.AddCommand(newJournalStatusCmd())
	cmd.AddCommand(newJournalSyncCmd())
	cmd.AddCommand(newJournalListCmd())
	cmd.AddCommand(newJournalTripsCmd())
	cmd.AddCommand(newJournalReviewCmd())
	return cmd
}

func newJournalStatusCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show journal sync state and size (does NOT contact IBKR)",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx := context.Background()
			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(err, exitSQLiteErr)
			}
			defer store.Close()

			st, err := store.GetSyncState(ctx)
			if err != nil {
				return cliExit(err, exitSQLiteErr)
			}
			all, err := store.ListExecutions(ctx, sqlite.JournalFilter{})
			if err != nil {
				return cliExit(err, exitSQLiteErr)
			}
			var earliest, latest time.Time
			if n := len(all); n > 0 {
				latest = all[0].Time
				earliest = all[n-1].Time
			}
			hours := 0.0
			gap := true
			if !st.LastSyncAt.IsZero() {
				hours = time.Since(st.LastSyncAt).Hours()
				gap = hours > server.GapWarningHours
			}
			payload := struct {
				LastSyncAt      string  `json:"last_sync_at"`
				HoursSinceSync  float64 `json:"hours_since_sync"`
				GapWarning      bool    `json:"gap_warning"`
				TotalExecutions int     `json:"total_executions"`
				EarliestRecord  string  `json:"earliest_record,omitempty"`
				LatestRecord    string  `json:"latest_record,omitempty"`
				LastError       string  `json:"last_error,omitempty"`
			}{
				LastSyncAt: isoOrEmpty(st.LastSyncAt), HoursSinceSync: hours,
				GapWarning: gap, TotalExecutions: len(all),
				EarliestRecord: isoOrEmpty(earliest), LatestRecord: isoOrEmpty(latest),
				LastError: st.LastError,
			}
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(payload)
			}
			fmt.Printf("Last sync:        %s\n", humanTime(st.LastSyncAt))
			if st.LastSyncAt.IsZero() {
				fmt.Printf("Time since sync:  (never synced)\n")
			} else {
				fmt.Printf("Time since sync:  %s\n", humanGap(hours))
			}
			fmt.Printf("Hours since sync: %.1f\n", hours)
			fmt.Printf("Gap warning:      %v\n", gap)
			fmt.Printf("Total executions: %d\n", len(all))
			fmt.Printf("Earliest record:  %s\n", humanTime(earliest))
			fmt.Printf("Latest record:    %s\n", humanTime(latest))
			if st.LastError != "" {
				fmt.Printf("Last error:       %s\n", st.LastError)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

func isoOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func humanTime(t time.Time) string {
	if t.IsZero() {
		return "(never)"
	}
	return t.Local().Format("2006-01-02 15:04:05 MST")
}

// humanGap pretty-prints "time since last sync" — days for ≥ 48h, otherwise hours.
// Keeps week-scale gaps from rendering as "144.0 hours".
func humanGap(hours float64) string {
	if hours >= 48 {
		return fmt.Sprintf("%.1f days", hours/24)
	}
	return fmt.Sprintf("%.1f hours", hours)
}

func errString(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

// connectJournalBroker connects using journalClientID (0, the IBKR implicit
// master). See the journalClientID const doc for the full rationale —
// journal needs cross-client execution visibility, which requires master
// status. Other subcommands use distinct non-zero IDs to avoid colliding
// with each other: quote(1), analyze(2), dashboard(3), positions(4),
// portfolio(5), `analyze --watchlist`(6), max-pain(8), chain(9). `trades`
// also uses ClientID 0 because it reads executions. Scheduler workers use 10+
// and the web pool 30+.
func connectJournalBroker(ctx context.Context) (broker.Broker, error) {
	b := factory.NewWithFallback(ibkr.Config{
		Host: ibHost, Port: ibPort, ClientID: journalClientID,
	}, pythonBin)
	if err := b.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect to broker: %w", err)
	}
	fmt.Fprintln(os.Stderr, b.SourceBanner())
	return b, nil
}

// bestEffortSync attempts a sync if a broker connection succeeds. The caller
// logs the returned error (if any) and proceeds with cached SQLite data.
func bestEffortSync(ctx context.Context, store *sqlite.Store) error {
	b, err := connectJournalBroker(ctx)
	if err != nil {
		return err
	}
	defer b.Disconnect()
	RegisterBrokerCleanup(b)
	market := server.NewMarketDataService(b, store)
	acct := server.NewAccountService(b, market)
	svc := server.NewJournalService(acct, store)
	_, err = svc.SyncExecutions(ctx)
	return err
}

func newJournalSyncCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Pull recent executions from IBKR into the journal",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx := context.Background()
			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(err, exitSQLiteErr)
			}
			defer store.Close()

			b, err := connectJournalBroker(ctx)
			if err != nil {
				return cliExit(err, exitIBKRUnreachable)
			}
			defer b.Disconnect()
			RegisterBrokerCleanup(b)

			market := server.NewMarketDataService(b, store)
			acct := server.NewAccountService(b, market)
			svc := server.NewJournalService(acct, store)

			n, syncErr := svc.SyncExecutions(ctx)
			st, _ := svc.SyncState(ctx)
			total, _ := store.ListExecutions(ctx, sqlite.JournalFilter{})

			hours, gap := 0.0, true
			if !st.LastSyncAt.IsZero() {
				hours = time.Since(st.LastSyncAt).Hours()
				gap = hours > server.GapWarningHours
			}
			payload := struct {
				NewCount   int     `json:"new_count"`
				TotalCount int     `json:"total_count"`
				LastSyncAt string  `json:"last_sync_at"`
				HoursSince float64 `json:"hours_since_sync"`
				GapWarning bool    `json:"gap_warning"`
				IBKROK     bool    `json:"ibkr_ok"`
				Error      string  `json:"error,omitempty"`
			}{n, len(total), isoOrEmpty(st.LastSyncAt), hours, gap, syncErr == nil, errString(syncErr)}

			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(payload)
			} else if syncErr != nil {
				fmt.Fprintf(os.Stderr, "⚠ Sync failed: %v\n", syncErr)
			} else {
				fmt.Printf("Synced %d new executions. Total: %d. Last sync: %s\n",
					n, len(total), humanTime(st.LastSyncAt))
			}
			if syncErr != nil {
				return cliExit(syncErr, exitIBKRUnreachable)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

func newJournalListCmd() *cobra.Command {
	var symbol, side, secType, since, until, format string
	var limit int
	var noSync bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List persisted executions (auto-syncs unless --no-sync)",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx := context.Background()
			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(err, exitSQLiteErr)
			}
			defer store.Close()

			if !noSync {
				if syncErr := bestEffortSync(ctx, store); syncErr != nil {
					fmt.Fprintf(os.Stderr, "⚠ auto-sync failed: %v (showing cached data)\n", syncErr)
				}
			}
			f, err := buildJournalFilter(symbol, side, secType, since, until, limit)
			if err != nil {
				return cliExit(err, exitGenericErr)
			}
			execs, err := store.ListExecutions(ctx, f)
			if err != nil {
				return cliExit(err, exitSQLiteErr)
			}
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Executions []model.Execution `json:"executions"`
				}{execs})
			}
			renderExecutionsTable(execs)
			return nil
		},
	}
	cmd.Flags().StringVar(&symbol, "symbol", "", "Filter by symbol")
	cmd.Flags().StringVar(&side, "side", "", "Filter by side: BOT | SLD")
	cmd.Flags().StringVar(&secType, "type", "", "Filter by sec type: stk | opt")
	cmd.Flags().StringVar(&since, "since", "", "Earliest date YYYY-MM-DD")
	cmd.Flags().StringVar(&until, "until", "", "Latest date YYYY-MM-DD")
	cmd.Flags().IntVar(&limit, "limit", 0, "Max rows (0 = no limit)")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip Layer-1 auto-sync")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

func buildJournalFilter(symbol, side, secType, since, until string, limit int) (sqlite.JournalFilter, error) {
	var f sqlite.JournalFilter
	f.Symbol, f.Side, f.SecType, f.Limit = symbol, side, secType, limit
	if since != "" {
		t, err := time.Parse("2006-01-02", since)
		if err != nil {
			return f, fmt.Errorf("invalid --since %q: use YYYY-MM-DD", since)
		}
		f.Since = t
	}
	if until != "" {
		t, err := time.Parse("2006-01-02", until)
		if err != nil {
			return f, fmt.Errorf("invalid --until %q: use YYYY-MM-DD", until)
		}
		f.Until = t.Add(24*time.Hour - time.Second)
	}
	return f, nil
}

func renderExecutionsTable(execs []model.Execution) {
	if len(execs) == 0 {
		fmt.Println("No executions match the filter.")
		return
	}
	fmt.Printf("%-18s %-8s %-5s %8s %10s %-10s %-8s\n",
		"Time", "Symbol", "Side", "Qty", "Price", "Exchange", "Account")
	for _, e := range execs {
		ts := "—"
		if !e.Time.IsZero() {
			ts = e.Time.Format("2006-01-02 15:04")
		}
		fmt.Printf("%-18s %-8s %-5s %8.0f %10.2f %-10s %-8s\n",
			ts, e.Symbol, e.Side, e.Shares, e.Price, e.Exchange, e.Account)
	}
}

func newJournalTripsCmd() *cobra.Command {
	var symbol, status, since, until, format string
	var noSync bool
	cmd := &cobra.Command{
		Use:   "trips",
		Short: "List round trips (FIFO-matched lifecycles with realized P&L)",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx := context.Background()
			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(err, exitSQLiteErr)
			}
			defer store.Close()

			if !noSync {
				if syncErr := bestEffortSync(ctx, store); syncErr != nil {
					fmt.Fprintf(os.Stderr, "⚠ auto-sync failed: %v (showing cached data)\n", syncErr)
				}
			}
			f := server.RoundTripFilter{Symbol: symbol, Status: status}
			if since != "" {
				t, err := time.Parse("2006-01-02", since)
				if err != nil {
					return cliExit(err, exitGenericErr)
				}
				f.Since = t
			}
			if until != "" {
				t, err := time.Parse("2006-01-02", until)
				if err != nil {
					return cliExit(err, exitGenericErr)
				}
				f.Until = t.Add(24*time.Hour - time.Second)
			}
			svc := server.NewJournalService(nil, store) // read-only path
			trips, err := svc.ListRoundTrips(ctx, f)
			if err != nil {
				return cliExit(err, exitSQLiteErr)
			}
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					RoundTrips []model.RoundTrip `json:"round_trips"`
				}{trips})
			}
			renderTripsTable(trips)
			return nil
		},
	}
	cmd.Flags().StringVar(&symbol, "symbol", "", "Filter by symbol")
	cmd.Flags().StringVar(&status, "status", "", "open | closed | expired")
	cmd.Flags().StringVar(&since, "since", "", "Earliest close date YYYY-MM-DD (filters by trip CloseTime)")
	cmd.Flags().StringVar(&until, "until", "", "Latest close date YYYY-MM-DD (filters by trip CloseTime)")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip Layer-1 auto-sync")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

func renderTripsTable(trips []model.RoundTrip) {
	if len(trips) == 0 {
		fmt.Println("No round trips match the filter.")
		return
	}
	fmt.Printf("%-8s %-5s %-3s %-16s %-16s %8s %10s %10s %10s %-8s\n",
		"Symbol", "Dir", "Ccy", "OpenTime", "CloseTime", "Qty", "OpenPx", "ClosePx", "PnL", "Status")
	for _, t := range trips {
		closeStr, closePx := "—", 0.0
		if !t.CloseTime.IsZero() {
			closeStr = t.CloseTime.Format("2006-01-02 15:04")
			closePx = t.CloseAvgPrice
		}
		fmt.Printf("%-8s %-5s %-3s %-16s %-16s %8.0f %10.2f %10.2f %10.2f %-8s\n",
			t.Symbol, t.Direction, model.NormalizeCurrency(t.Currency),
			t.OpenTime.Format("2006-01-02 15:04"), closeStr,
			t.OpenQty, t.OpenAvgPrice, closePx, t.RealizedPnL, t.Status)
	}
}

func newJournalReviewCmd() *cobra.Command {
	var since, until, format string
	var noSync bool
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Retrospective summary: win rate, total P&L, by-symbol breakdown",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx := context.Background()
			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(err, exitSQLiteErr)
			}
			defer store.Close()

			if !noSync {
				if syncErr := bestEffortSync(ctx, store); syncErr != nil {
					fmt.Fprintf(os.Stderr, "⚠ auto-sync failed: %v (showing cached data)\n", syncErr)
				}
			}
			var sinceT, untilT time.Time
			if since != "" {
				t, err := time.Parse("2006-01-02", since)
				if err != nil {
					return cliExit(err, exitGenericErr)
				}
				sinceT = t
			}
			if until != "" {
				t, err := time.Parse("2006-01-02", until)
				if err != nil {
					return cliExit(err, exitGenericErr)
				}
				untilT = t.Add(24*time.Hour - time.Second)
			}
			svc := server.NewJournalService(nil, store)
			sum, err := svc.Review(ctx, sinceT, untilT)
			if err != nil {
				return cliExit(err, exitSQLiteErr)
			}
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(sum)
			}
			renderReview(sum)
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "Earliest close date YYYY-MM-DD (filters by trip CloseTime)")
	cmd.Flags().StringVar(&until, "until", "", "Latest close date YYYY-MM-DD (filters by trip CloseTime)")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip Layer-1 auto-sync")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

func renderReview(s model.ReviewSummary) {
	fmt.Println("=== Trade Review ===")
	if !s.Since.IsZero() || !s.Until.IsZero() {
		fmt.Printf("Window: %s → %s\n", humanTime(s.Since), humanTime(s.Until))
	}
	fmt.Printf("Total executions:   %d\n", s.TotalExecutions)
	fmt.Printf("Total round trips:  %d  (closed: %d, open: %d, expired: %d)\n",
		s.TotalRoundTrips, s.ClosedRoundTrips, s.OpenRoundTrips, s.ExpiredRoundTrips)
	if s.ExcludedNonUSDRoundTrips > 0 {
		fmt.Printf("Excluded non-USD:   %d  (review P&L is USD-only)\n", s.ExcludedNonUSDRoundTrips)
	}
	fmt.Printf("Win rate:           %.1f%%  (W: %d, L: %d)\n", s.WinRate*100, s.WinCount, s.LossCount)
	fmt.Printf("Total realized P&L: %.2f USD\n", s.TotalRealizedPnL)
	fmt.Printf("Avg per round trip: %.2f\n", s.AvgRealizedPnL)
	fmt.Printf("Avg holding days:   %.1f\n", s.AvgHoldingDays)
	if len(s.BySymbol) > 0 {
		fmt.Println("\nBy symbol:")
		fmt.Printf("  %-8s %6s %10s %8s\n", "Symbol", "Trips", "P&L USD", "Win%")
		for _, ss := range s.BySymbol {
			fmt.Printf("  %-8s %6d %10.2f %7.1f%%\n",
				ss.Symbol, ss.RoundTripCount, ss.RealizedPnL, ss.WinRate*100)
		}
	}
}
