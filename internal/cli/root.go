package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

var (
	cfgFile   string
	dbPath    string
	ibHost    string
	ibPortRaw string
	ibPort    int
	pythonBin string

	defaultAnalysisAddr = "localhost:50052"
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
		sig := <-ch
		runCleanup()
		os.Exit(signalExitCode(sig))
	}()
}

func signalExitCode(sig os.Signal) int {
	switch sig {
	case syscall.SIGINT:
		return 130
	case syscall.SIGTERM:
		return 143
	default:
		return 1
	}
}

// version is set by main() via SetVersion. Build-time via:
//
//	go build -ldflags="-X main.version=v1.2.3"
var version = "dev"

// SetVersion lets cmd/optix-cli/main.go pass through its build-time version
// string. Called once at startup before Execute().
func SetVersion(v string) {
	if v != "" {
		version = v
	}
}

// NewRootCmd creates the root cobra command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "optix",
		Short:   "US stock & options strategy analysis tool",
		Long:    "Optix analyzes stocks and options to recommend sell-side strategies for the upcoming expiration.",
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cleanupOnce.Do(initSignalHandler)
			if err := applyRootConfig(changedFlags(cmd)); err != nil {
				return err
			}
			p, err := resolveIBPort(ibPortRaw)
			if err != nil {
				return fmt.Errorf("invalid --ib-port %q: use gateway, tws, or a number", ibPortRaw)
			}
			ibPort = p
			return nil
		},
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "configs/optix.yaml", "root config file; missing file uses built-in defaults")
	root.PersistentFlags().StringVar(&dbPath, "db", "./data/optix.db", "SQLite database path")
	root.PersistentFlags().StringVar(&ibHost, "ib-host", "127.0.0.1", "IB Gateway/TWS host")
	root.PersistentFlags().StringVar(&ibPortRaw, "ib-port", "gateway", "IB port: gateway (4001), tws (7496), or number")
	root.PersistentFlags().StringVar(&pythonBin, "python", "python3", "Python interpreter for yfinance fallback")

	root.AddCommand(newQuoteCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newDashboardCmd())
	root.AddCommand(newAnalyzeCmd())
	root.AddCommand(newChainCmd())
	root.AddCommand(newPositionsCmd())
	root.AddCommand(newPortfolioCmd())
	root.AddCommand(newTradesCmd())
	root.AddCommand(newJournalCmd())
	root.AddCommand(newMaxPainCmd())
	root.AddCommand(newPulseCmd())
	root.AddCommand(newIntelCmd())
	root.AddCommand(newPremarketCmd())
	root.AddCommand(newPostcloseCmd())
	root.AddCommand(newServerCmd())

	return root
}

type rootConfig struct {
	DBPath             string
	IBHost             string
	IBPort             string
	PythonAnalysisAddr string
}

type rootConfigYAML struct {
	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
	GRPC struct {
		PythonServerAddr string `yaml:"python_server_addr"`
	} `yaml:"grpc"`
	IBKR struct {
		Host string `yaml:"host"`
		Port any    `yaml:"port"`
	} `yaml:"ibkr"`
}

func changedFlags(cmd *cobra.Command) map[string]bool {
	changed := map[string]bool{}
	for _, name := range []string{"db", "ib-host", "ib-port", "analysis-addr"} {
		if cmd.Flags().Changed(name) || cmd.InheritedFlags().Changed(name) {
			changed[name] = true
		}
	}
	return changed
}

func resolveAnalysisAddr(cmd *cobra.Command, current string) string {
	if cmd.Flags().Changed("analysis-addr") || cmd.InheritedFlags().Changed("analysis-addr") {
		return current
	}
	return defaultAnalysisAddr
}

func applyRootConfig(changed map[string]bool) error {
	cfg, err := loadRootConfig(cfgFile)
	if err != nil {
		return err
	}
	if !changed["db"] && cfg.DBPath != "" {
		dbPath = cfg.DBPath
	}
	if !changed["ib-host"] && cfg.IBHost != "" {
		ibHost = cfg.IBHost
	}
	if !changed["ib-port"] && cfg.IBPort != "" {
		ibPortRaw = cfg.IBPort
	}
	if !changed["analysis-addr"] && cfg.PythonAnalysisAddr != "" {
		defaultAnalysisAddr = cfg.PythonAnalysisAddr
	}
	return nil
}

func loadRootConfig(path string) (rootConfig, error) {
	var cfg rootConfig
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	return parseRootConfigYAML(string(data))
}

func parseRootConfigYAML(s string) (rootConfig, error) {
	var cfg rootConfig
	var raw rootConfigYAML
	if err := yaml.Unmarshal([]byte(s), &raw); err != nil {
		return cfg, fmt.Errorf("parse config YAML: %w", err)
	}
	cfg.DBPath = raw.Database.Path
	cfg.IBHost = raw.IBKR.Host
	if raw.IBKR.Port != nil {
		cfg.IBPort = fmt.Sprint(raw.IBKR.Port)
	}
	cfg.PythonAnalysisAddr = raw.GRPC.PythonServerAddr
	return cfg, nil
}

// Execute runs the root command. Returns the error from cobra so the caller
// (cmd/optix-cli/main.go) can translate it to a documented exit code via
// AsExitCode.
func Execute() error {
	cmd := NewRootCmd()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}
