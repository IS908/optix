package cli

import (
	"context"
	"fmt"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/server"
	"github.com/spf13/cobra"
)

func newQuoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quote [symbol]",
		Short: "Get the latest stock quote",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			ctx := context.Background()

			store, err := sqlite.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			RegisterCleanup(store)
			defer store.Close()

			b := broker.NewWithFallback(ibkr.Config{
				Host:     ibHost,
				Port:     ibPort,
				ClientID: 1,
			}, pythonBin)
			if err := b.Connect(ctx); err != nil {
				return fmt.Errorf("connect to broker: %w", err)
			}
			defer b.Disconnect()
			fmt.Println(b.SourceBanner())

			svc := server.NewMarketDataService(b, store)
			q, err := svc.GetQuote(ctx, symbol)
			if err != nil {
				return err
			}

			fmt.Printf("%-8s %s\n", "Symbol:", q.Symbol)
			fmt.Printf("%-8s %.2f\n", "Last:", q.Last)
			fmt.Printf("%-8s %.2f\n", "Bid:", q.Bid)
			fmt.Printf("%-8s %.2f\n", "Ask:", q.Ask)
			fmt.Printf("%-8s %d\n", "Volume:", q.Volume)
			fmt.Printf("%-8s %.2f (%.2f%%)\n", "Change:", q.Change, q.ChangePct)
			fmt.Printf("%-8s %s\n", "Time:", q.Timestamp.Format("2006-01-02 15:04:05"))
			return nil
		},
	}
}
