package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/server"
	"github.com/spf13/cobra"
)

func newQuoteCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "quote [symbol]",
		Short: "Get the latest stock quote",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			format = strings.ToLower(format)
			symbol := args[0]
			ctx := context.Background()

			store, err := sqlite.New(dbPath)
			if err != nil {
				return cliExit(fmt.Errorf("open database: %w", err), exitSQLiteErr)
			}
			RegisterCleanup(store)
			defer store.Close()

			b := factory.NewWithFallback(ibkr.Config{
				Host:     ibHost,
				Port:     ibPort,
				ClientID: 1,
			}, pythonBin)
			if err := b.Connect(ctx); err != nil {
				return cliExit(fmt.Errorf("connect to broker: %w", err), exitIBKRUnreachable)
			}
			defer b.Disconnect()
			if format == "json" {
				fmt.Fprintln(os.Stderr, b.SourceBanner())
			} else {
				fmt.Println(b.SourceBanner())
			}

			svc := server.NewMarketDataService(b, store)
			q, err := svc.GetQuote(ctx, symbol)
			if err != nil {
				return cliExit(err, exitIBKRUnreachable)
			}

			if format == "json" {
				return renderQuoteJSON(os.Stdout, q, b.SourceName())
			}

			fmt.Printf("%-10s %s\n", "Symbol:", q.Symbol)
			fmt.Printf("%-10s %.2f\n", "Last:", q.Last)
			fmt.Printf("%-10s %.2f\n", "Bid:", q.Bid)
			fmt.Printf("%-10s %.2f\n", "Ask:", q.Ask)
			fmt.Printf("%-10s %d\n", "Volume:", q.Volume)
			fmt.Printf("%-10s %.2f (%.2f%%)\n", "Change:", q.Change, q.ChangePct)
			fmt.Printf("%-10s %s\n", "Time:", q.Timestamp.Format("2006-01-02 15:04:05"))
			fmt.Printf("%-10s %s\n", "Session:", q.MarketSession.Label())
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}
