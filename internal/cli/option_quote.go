package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/pkg/model"
	"github.com/spf13/cobra"
)

// optionQuoteClientID is reserved for focused single-contract validation.
// Keep it below scheduler worker IDs (10+) and distinct from other CLI slots.
const optionQuoteClientID = 7

func newOptionQuoteCmd() *cobra.Command {
	var expiry string
	var right string
	var strike float64
	var format string
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:           "option-quote [symbol]",
		Short:         "Fetch a single option contract quote from IBKR",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			format = strings.ToLower(format)
			if expiry == "" {
				return fmt.Errorf("--expiry is required")
			}
			expiryCompact, err := parseExpiryFlag(expiry, time.Now())
			if err != nil {
				return err
			}
			right = strings.ToUpper(strings.TrimSpace(right))
			if right != "C" && right != "P" && right != "CALL" && right != "PUT" {
				return fmt.Errorf("--right must be C/call or P/put")
			}
			if strike <= 0 {
				return fmt.Errorf("--strike must be positive")
			}

			symbol := strings.ToUpper(args[0])
			ctx := context.Background()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			b := ibkr.New(ibkr.Config{
				Host:     ibHost,
				Port:     ibPort,
				ClientID: optionQuoteClientID,
			})
			if err := b.Connect(ctx); err != nil {
				return cliExit(fmt.Errorf("connect to IBKR: %w", err), exitIBKRUnreachable)
			}
			defer b.Disconnect()
			RegisterBrokerCleanup(b)

			quoter, ok := any(b).(broker.DetailedOptionQuoter)
			if !ok {
				return cliExit(fmt.Errorf("broker does not support detailed option quotes"), exitIBKRUnreachable)
			}
			q, err := quoter.GetOptionQuoteDetails(ctx, symbol, expiryCompact, right, strike)
			if err != nil {
				return cliExit(fmt.Errorf("fetch option quote: %w", err), exitIBKRUnreachable)
			}

			if format == "json" {
				if err := renderOptionQuoteJSON(os.Stdout, q, "IBKR"); err != nil {
					return err
				}
			} else {
				renderOptionQuoteText(os.Stdout, q)
			}
			if !optionQuoteHasPriceData(q) {
				return optionQuoteFailureExit(q)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&expiry, "expiry", "", "Option expiration (YYYY-MM-DD)")
	cmd.Flags().StringVar(&right, "right", "", "Option right: C/call or P/put")
	cmd.Flags().Float64Var(&strike, "strike", 0, "Option strike")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Overall command timeout, e.g. 20s (0 = no explicit timeout; the internal per-request collection window still applies). For scanner-style callers that need a hard subprocess budget.")
	return cmd
}

// optionQuoteFailureExit builds the CLI error for a quote with no usable
// price data, choosing the exit code by failure class (#193 finding 6a):
//
//   - IBKR responded with an explicit per-request error (e.g. errCode 200
//     "no security definition" for a bad strike/expiry/right) — the gateway
//     is reachable and working, the *contract* is what's invalid. Reports
//     that real IB error as the headline message (finding 1) under
//     exitNoData, so scanner-style callers can branch on exit code alone
//     instead of string-matching stderr.
//   - Otherwise (still connected, but the collection window elapsed with no
//     IB error — e.g. no real-time subscription, a thin/expired contract,
//     or a request that simply timed out) falls back to the full
//     structured warnings list, preserving today's behavior and exit code.
func optionQuoteFailureExit(q *model.OptionQuote) error {
	if ibErr := ibkrErrorDetail(q); ibErr != "" {
		return cliExit(fmt.Errorf("option quote unavailable: %s", ibErr), exitNoData)
	}
	return cliExit(fmt.Errorf("option quote has no usable price data; warnings: %s", strings.Join(optionQuoteValidationWarnings(q), ", ")), exitIBKRUnreachable)
}

func optionQuoteHasPriceData(q *model.OptionQuote) bool {
	if q == nil {
		return false
	}
	return q.Mark > 0 || q.Mid > 0 || q.Last > 0 || (q.Bid > 0 && q.Ask > 0)
}

func renderOptionQuoteText(w io.Writer, q *model.OptionQuote) {
	if q == nil {
		fmt.Fprintln(w, "No option quote returned.")
		return
	}
	fmt.Fprintf(w, "Option quote for %s %s %s %.2f\n", q.Underlying, dashed(q.Expiration), optionRight(q.OptionType), q.Strike)
	fmt.Fprintln(w, "Source: IBKR")
	fmt.Fprintf(w, "%-18s %.2f\n", "Last:", q.Last)
	fmt.Fprintf(w, "%-18s %.2f\n", "Bid:", q.Bid)
	fmt.Fprintf(w, "%-18s %.2f\n", "Ask:", q.Ask)
	fmt.Fprintf(w, "%-18s %.2f\n", "Mid:", q.Mid)
	fmt.Fprintf(w, "%-18s %.2f\n", "Mark:", q.Mark)
	fmt.Fprintf(w, "%-18s %d\n", "Volume:", q.Volume)
	fmt.Fprintf(w, "%-18s %d\n", "Open Interest:", q.OpenInterest)
	fmt.Fprintf(w, "%-18s %.4f\n", "IV:", q.ImpliedVolatility)
	fmt.Fprintf(w, "%-18s %s\n", "Market Data:", optionMarketDataType(q.MarketDataType))
	if warnings := optionQuoteValidationWarnings(q); len(warnings) > 0 {
		fmt.Fprintf(w, "%-18s %s\n", "Warnings:", strings.Join(warnings, ", "))
	}
}
