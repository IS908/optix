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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/IS908/optix/internal/broker"
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

// closerFunc adapts a cleanup func to io.Closer for RegisterCleanup.
type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// RegisterBrokerCleanup 把 broker 的 Disconnect 挂进信号清理注册表:
// SIGTERM/SIGINT 走 os.Exit 时 defer 不执行,不注册的话 IB Gateway 侧会话
// 成为僵尸、占住 clientID,后续连接触发 326 → fallback ID 堆积。
func RegisterBrokerCleanup(b broker.Broker) {
	RegisterCleanup(closerFunc(b.Disconnect))
}

func runCleanup() {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	for i := len(cleanupItems) - 1; i >= 0; i-- {
		cleanupItems[i].Close()
	}
	cleanupItems = nil
}

// osExit is os.Exit through an indirection so tests can observe whether (and
// with what code) the signal handler would have terminated the process,
// without actually killing the test binary.
var osExit = os.Exit

// signalExitSuppressed is set by SuppressSignalExit when a command installs
// its own SIGINT/SIGTERM-driven graceful-shutdown flow (currently: `optix
// server` via signal.NotifyContext in server.go) that must exclusively own
// the shutdown decision. See handleSignal for why this matters (#196).
var signalExitSuppressed atomic.Bool

// SuppressSignalExit tells the root SIGINT/SIGTERM handler installed by
// initSignalHandler to stand down: on the next signal it will neither run
// the cleanup registry nor call os.Exit, leaving the calling command's own
// signal-driven shutdown (e.g. signal.NotifyContext) as the sole driver of
// both cleanup and process exit.
//
// Why this is needed (#196): initSignalHandler runs for every command via
// PersistentPreRunE, so `optix server` had TWO independent SIGTERM listeners
// — this one and its own NotifyContext-based graceful shutdown (draining
// HTTP connections, closing the broker pool). Go delivers a signal to every
// channel registered via signal.Notify, so both fired concurrently: the root
// handler ran cleanup (closing the SQLite store immediately) and called
// os.Exit within ~5s regardless of whether the server's own HTTP drain /
// pool close had finished, truncating it. Call this once, early, before the
// server starts accepting a shutdown signal — the store close and broker
// pool close still happen, just exclusively via the server's own deferred
// shutdown sequence (see cli/server.go) instead of racing this handler.
func SuppressSignalExit() {
	signalExitSuppressed.Store(true)
}

func initSignalHandler() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		handleSignal(<-ch)
	}()
}

// handleSignal runs the root package's default signal response: flush
// registered cleanup (store, broker connections) and exit with a signal-
// appropriate code, bounded by a 5s watchdog in case cleanup blocks (e.g.
// ibapi Disconnect during a Gateway hang).
//
// When signalExitSuppressed is set, this is a deliberate no-op — see
// SuppressSignalExit for why.
func handleSignal(sig os.Signal) {
	if signalExitSuppressed.Load() {
		return
	}
	done := make(chan struct{})
	go func() {
		runCleanup()
		close(done)
	}()
	// ibapi Disconnect 可能在 gateway 假死时阻塞;5s 看门狗保证进程一定退出。
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	osExit(signalExitCode(sig))
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
	root.PersistentFlags().StringVar(&dbPath, "db", "", "SQLite database path (flag > OPTIX_DB_PATH > YAML > user data directory)")
	root.PersistentFlags().StringVar(&ibHost, "ib-host", "127.0.0.1", "IB Gateway/TWS host")
	root.PersistentFlags().StringVar(&ibPortRaw, "ib-port", "gateway", "IB port: gateway (4001), tws (7496), or number")
	root.PersistentFlags().StringVar(&pythonBin, "python", defaultPython(), "Python interpreter for yfinance (defaults to project venv when available)")

	root.AddCommand(newDataCmd())
	root.AddCommand(newQuoteCmd())
	root.AddCommand(newOptionQuoteCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newDashboardCmd())
	root.AddCommand(newAnalyzeCmd())
	root.AddCommand(newChainCmd())
	root.AddCommand(newPositionsCmd())
	root.AddCommand(newPortfolioCmd())
	root.AddCommand(newTradesCmd())
	root.AddCommand(newJournalCmd())
	root.AddCommand(newScanJournalCmd())
	root.AddCommand(newMaxPainCmd())
	root.AddCommand(newPulseCmd())
	root.AddCommand(newIntelCmd())
	root.AddCommand(newPremarketCmd())
	root.AddCommand(newPostcloseCmd())
	root.AddCommand(newEventCmd())
	root.AddCommand(newShockCmd())
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
	resolved, err := resolveDatabase(changed, cfg.DBPath, true)
	if err != nil {
		return err
	}
	dbPath = resolved.Path
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
