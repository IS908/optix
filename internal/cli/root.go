package cli

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	cfgFile    string
	dbPath     string
	ibHost     string
	ibPortRaw  string
	ibPort     int
	pythonBin  string
)

// resolveIBPort maps port aliases to numeric values.
//
//	"gateway" -> 4001, "tws" -> 7496, otherwise parse as integer.
func resolveIBPort(raw string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "gateway":
		return 4001, nil
	case "tws":
		return 7496, nil
	default:
		return strconv.Atoi(raw)
	}
}

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
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cleanupOnce.Do(initSignalHandler)
			p, err := resolveIBPort(ibPortRaw)
			if err != nil {
				return fmt.Errorf("invalid --ib-port %q: use gateway, tws, or a number", ibPortRaw)
			}
			ibPort = p
			return nil
		},
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./configs/optix.yaml)")
	root.PersistentFlags().StringVar(&dbPath, "db", "./data/optix.db", "SQLite database path")
	root.PersistentFlags().StringVar(&ibHost, "ib-host", "127.0.0.1", "IB TWS/Gateway host")
	root.PersistentFlags().StringVar(&ibPortRaw, "ib-port", "gateway", "IB port: gateway (4001), tws (7496), or number")
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
