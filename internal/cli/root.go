package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	dbPath  string
	ibHost  string
	ibPort  int
)

// NewRootCmd creates the root cobra command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "optix",
		Short: "US stock & options strategy analysis tool",
		Long:  "Optix analyzes stocks and options to recommend sell-side strategies for the upcoming expiration.",
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./configs/optix.yaml)")
	root.PersistentFlags().StringVar(&dbPath, "db", "./data/optix.db", "SQLite database path")
	root.PersistentFlags().StringVar(&ibHost, "ib-host", "127.0.0.1", "IB TWS/Gateway host")
	root.PersistentFlags().IntVar(&ibPort, "ib-port", 7497, "IB TWS/Gateway port")

	root.AddCommand(newQuoteCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newDashboardCmd())
	root.AddCommand(newAnalyzeCmd())
	root.AddCommand(newChainCmd())
	root.AddCommand(newServerCmd())

	return root
}

// Execute runs the root command.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
