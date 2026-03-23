package cli

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	cfgFile   string
	dbPath    string
	ibHost    string
	ibPort    int
	pythonBin string
)

// cleanupRegistry collects io.Closer instances (store, broker) so that
// SIGTERM / SIGINT can flush WAL and release resources even when defer
// statements are skipped.
var (
	cleanupMu    sync.Mutex
	cleanupItems []io.Closer
	cleanupOnce  sync.Once
)

// RegisterCleanup adds a Closer to be called on process exit signals.
func RegisterCleanup(c io.Closer) {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	cleanupItems = append(cleanupItems, c)
}

func runCleanup() {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	for i := len(cleanupItems) - 1; i >= 0; i-- {
		cleanupItems[i].Close()
	}
	cleanupItems = nil
}

func initSignalHandler() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		runCleanup()
		os.Exit(0)
	}()
}

// NewRootCmd creates the root cobra command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "optix",
		Short: "US stock & options strategy analysis tool",
		Long:  "Optix analyzes stocks and options to recommend sell-side strategies for the upcoming expiration.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cleanupOnce.Do(initSignalHandler)
		},
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./configs/optix.yaml)")
	root.PersistentFlags().StringVar(&dbPath, "db", "./data/optix.db", "SQLite database path")
	root.PersistentFlags().StringVar(&ibHost, "ib-host", "127.0.0.1", "IB TWS/Gateway host")
	root.PersistentFlags().IntVar(&ibPort, "ib-port", 7496, "IB TWS/Gateway port")
	root.PersistentFlags().StringVar(&pythonBin, "python", "python3", "Python interpreter for yfinance fallback")

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
