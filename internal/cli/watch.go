package cli

import (
	"context"
	"fmt"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/watchlist"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Manage your watchlist",
	}

	cmd.AddCommand(newWatchAddCmd())
	cmd.AddCommand(newWatchRemoveCmd())
	cmd.AddCommand(newWatchListCmd())

	return cmd
}

func newWatchAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add [symbols...]",
		Short: "Add symbols to your watchlist",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			store, err := sqlite.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			RegisterCleanup(store)
			defer store.Close()

			mgr := watchlist.NewManager(store)
			if err := mgr.Add(ctx, args...); err != nil {
				return err
			}

			for _, s := range args {
				fmt.Printf("Added %s to watchlist\n", s)
			}
			return nil
		},
	}
}

func newWatchRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [symbol]",
		Short: "Remove a symbol from your watchlist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			store, err := sqlite.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			RegisterCleanup(store)
			defer store.Close()

			symbol := args[0]
			mgr := watchlist.NewManager(store)
			if err := mgr.Remove(ctx, symbol); err != nil {
				return err
			}

			// Cascade: clean up related data for the removed symbol
			_ = store.DeleteWatchlistSnapshots(ctx, symbol)
			_ = store.DeleteAnalysisCache(ctx, symbol)
			_ = store.DeleteBackgroundJobs(ctx, symbol)

			fmt.Printf("Removed %s from watchlist\n", symbol)
			return nil
		},
	}
}

func newWatchListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show your watchlist",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			store, err := sqlite.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			RegisterCleanup(store)
			defer store.Close()

			mgr := watchlist.NewManager(store)
			items, err := mgr.List(ctx)
			if err != nil {
				return err
			}

			if len(items) == 0 {
				fmt.Println("Watchlist is empty. Use 'optix watch add AAPL TSLA' to add symbols.")
				return nil
			}

			fmt.Printf("%-8s %-20s %s\n", "Symbol", "Added", "Notes")
			fmt.Println("------   --------------------  -----")
			for _, item := range items {
				fmt.Printf("%-8s %-20s %s\n", item.Symbol, item.AddedAt, item.Notes)
			}
			return nil
		},
	}
}
