package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newChainCmd() *cobra.Command {
	var expiry string

	cmd := &cobra.Command{
		Use:   "chain [symbol]",
		Short: "Show option chain for a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			// TODO: Implement option chain display (Phase 3)
			fmt.Printf("Option chain for %s", symbol)
			if expiry != "" {
				fmt.Printf(" (expiry: %s)", expiry)
			}
			fmt.Println()
			fmt.Println("(Not yet implemented - coming in Phase 3)")
			return nil
		},
	}

	cmd.Flags().StringVar(&expiry, "expiry", "", "Filter to specific expiration (YYYY-MM-DD)")

	return cmd
}
