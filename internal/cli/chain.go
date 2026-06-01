package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/pkg/model"
	"github.com/spf13/cobra"
)

const chainClientID = 9

func newChainCmd() *cobra.Command {
	var expiry string
	var format string

	cmd := &cobra.Command{
		Use:           "chain [symbol]",
		Short:         "Show option chain for a symbol",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			format = strings.ToLower(format)
			symbol := strings.ToUpper(args[0])
			ctx := context.Background()

			expiryCompact, err := parseExpiryFlag(expiry, time.Now())
			if err != nil {
				return err
			}

			b := factory.NewWithFallback(ibkr.Config{
				Host:     ibHost,
				Port:     ibPort,
				ClientID: chainClientID,
			}, pythonBin)
			if err := b.Connect(ctx); err != nil {
				return cliExit(fmt.Errorf("connect to broker: %w", err), exitIBKRUnreachable)
			}
			defer b.Disconnect()
			fmt.Fprintln(os.Stderr, b.SourceBanner())

			chain, err := b.GetOptionChain(ctx, symbol, expiryCompact)
			if err != nil {
				var miss *broker.ErrExpiryNotAvailable
				if errors.As(err, &miss) {
					tmpl := fmt.Sprintf("optix chain %s --expiry %%s", symbol)
					return errors.New(FormatExpiryError(miss.Underlying, miss.Requested, miss.Available, tmpl))
				}
				return cliExit(fmt.Errorf("fetch chain for %s: %w", symbol, err), exitIBKRUnreachable)
			}

			if format == "json" {
				return renderOptionChainJSON(os.Stdout, chain, b.SourceName())
			}
			renderOptionChainText(os.Stdout, chain, b.SourceName())
			return nil
		},
	}

	cmd.Flags().StringVar(&expiry, "expiry", "", "Filter to specific expiration (YYYY-MM-DD)")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")

	return cmd
}

func renderOptionChainText(w io.Writer, chain *model.OptionChain, source string) {
	fmt.Fprintf(w, "Option chain for %s\n", chain.Underlying)
	if source != "" {
		fmt.Fprintf(w, "Source: %s\n", source)
	}
	if chain.UnderlyingPrice > 0 {
		fmt.Fprintf(w, "Underlying: $%.2f\n", chain.UnderlyingPrice)
	}
	if len(chain.Expirations) == 0 {
		fmt.Fprintln(w, "No option expirations returned.")
		return
	}

	for i, exp := range chain.Expirations {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "Expiry: %s", dashed(exp.Expiration))
		if exp.DaysToExpiry > 0 {
			fmt.Fprintf(w, " (%d days)", exp.DaysToExpiry)
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%8s %8s %8s %8s %8s %8s %8s %8s %8s %8s\n",
			"Strike", "CallBid", "CallAsk", "CallLast", "CallOI", "PutBid", "PutAsk", "PutLast", "PutOI", "Volume")

		rows := chainRows(exp)
		for _, row := range rows {
			fmt.Fprintf(w, "%8.2f %8s %8s %8s %8s %8s %8s %8s %8s %8s\n",
				row.strike,
				priceCell(row.call.Bid),
				priceCell(row.call.Ask),
				priceCell(row.call.Last),
				intCell(row.call.OpenInterest),
				priceCell(row.put.Bid),
				priceCell(row.put.Ask),
				priceCell(row.put.Last),
				intCell(row.put.OpenInterest),
				int64Cell(row.call.Volume+row.put.Volume),
			)
		}
	}
}

type chainRow struct {
	strike float64
	call   model.OptionQuote
	put    model.OptionQuote
}

func chainRows(exp model.OptionChainExpiry) []chainRow {
	byStrike := make(map[float64]*chainRow, len(exp.Calls)+len(exp.Puts))
	for _, c := range exp.Calls {
		row := byStrike[c.Strike]
		if row == nil {
			row = &chainRow{strike: c.Strike}
			byStrike[c.Strike] = row
		}
		row.call = c
	}
	for _, p := range exp.Puts {
		row := byStrike[p.Strike]
		if row == nil {
			row = &chainRow{strike: p.Strike}
			byStrike[p.Strike] = row
		}
		row.put = p
	}

	rows := make([]chainRow, 0, len(byStrike))
	for _, row := range byStrike {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].strike < rows[j].strike
	})
	return rows
}

func priceCell(v float64) string {
	if v <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", v)
}

func intCell(v int32) string {
	if v <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d", v)
}

func int64Cell(v int64) string {
	if v <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d", v)
}
