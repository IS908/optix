package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestApplyRootConfigUsesFileDefaultsAndFlagOverrides(t *testing.T) {
	path := writeRootConfigForTest(t)
	old := rootConfigState{
		cfgFile: cfgFile, dbPath: dbPath, ibHost: ibHost, ibPortRaw: ibPortRaw,
		defaultAnalysisAddr: defaultAnalysisAddr,
	}
	t.Cleanup(func() { restoreRootConfigState(old) })

	cfgFile = path
	dbPath = "./data/optix.db"
	ibHost = "127.0.0.1"
	ibPortRaw = "gateway"
	changed := map[string]bool{}
	if err := applyRootConfig(changed); err != nil {
		t.Fatal(err)
	}
	if dbPath != "/tmp/optix-config.db" {
		t.Fatalf("dbPath = %q, want config database.path", dbPath)
	}
	if ibHost != "192.0.2.10" {
		t.Fatalf("ibHost = %q, want config ibkr.host", ibHost)
	}
	if ibPortRaw != "7497" {
		t.Fatalf("ibPortRaw = %q, want config ibkr.port", ibPortRaw)
	}
	if defaultAnalysisAddr != "localhost:60000" {
		t.Fatalf("defaultAnalysisAddr = %q, want config grpc.python_server_addr", defaultAnalysisAddr)
	}

	dbPath = "/tmp/flag.db"
	ibHost = "flag-host"
	ibPortRaw = "tws"
	defaultAnalysisAddr = "flag-analysis"
	changed = map[string]bool{"db": true, "ib-host": true, "ib-port": true, "analysis-addr": true}
	if err := applyRootConfig(changed); err != nil {
		t.Fatal(err)
	}
	if dbPath != "/tmp/flag.db" || ibHost != "flag-host" || ibPortRaw != "tws" || defaultAnalysisAddr != "flag-analysis" {
		t.Fatalf("flag overrides not preserved: db=%q host=%q port=%q analysis=%q", dbPath, ibHost, ibPortRaw, defaultAnalysisAddr)
	}
}

func TestRootConfigParsesStandardYAMLStrings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "optix.yaml")
	data := []byte(`
ibkr:
  host: '192.0.2.20'
  port: "7497"
grpc:
  python_server_addr: 'localhost:61000'
database:
  path: '/tmp/optix-standard-yaml.db'
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadRootConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IBHost != "192.0.2.20" || cfg.IBPort != "7497" || cfg.PythonAnalysisAddr != "localhost:61000" || cfg.DBPath != "/tmp/optix-standard-yaml.db" {
		t.Fatalf("parsed config = %+v", cfg)
	}
}

func TestRootCommandAppliesConfigBeforeChildRun(t *testing.T) {
	path := writeRootConfigForTest(t)
	old := rootConfigState{
		cfgFile: cfgFile, dbPath: dbPath, ibHost: ibHost, ibPortRaw: ibPortRaw,
		defaultAnalysisAddr: defaultAnalysisAddr,
	}
	t.Cleanup(func() { restoreRootConfigState(old) })

	dbPath = "./data/optix.db"
	ibHost = "127.0.0.1"
	ibPortRaw = "gateway"
	defaultAnalysisAddr = "localhost:50052"

	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	var got rootConfigState
	cmd.AddCommand(&cobra.Command{
		Use: "probe",
		RunE: func(_ *cobra.Command, _ []string) error {
			got = rootConfigState{
				cfgFile: cfgFile, dbPath: dbPath, ibHost: ibHost, ibPortRaw: ibPortRaw,
				defaultAnalysisAddr: defaultAnalysisAddr,
			}
			return nil
		},
	})
	cmd.SetArgs([]string{"--config", path, "--db", "/tmp/flag-wins.db", "probe"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if got.dbPath != "/tmp/flag-wins.db" {
		t.Fatalf("dbPath = %q, want explicit flag value", got.dbPath)
	}
	if got.ibHost != "192.0.2.10" || got.ibPortRaw != "7497" || got.defaultAnalysisAddr != "localhost:60000" {
		t.Fatalf("config not applied before child run: %+v", got)
	}
	if ibPort != 7497 {
		t.Fatalf("ibPort = %d, want resolved config port", ibPort)
	}
}

func TestResolveAnalysisAddrUsesConfigUnlessFlagChanged(t *testing.T) {
	old := rootConfigState{defaultAnalysisAddr: defaultAnalysisAddr}
	t.Cleanup(func() { restoreRootConfigState(old) })
	defaultAnalysisAddr = "localhost:60000"

	cmd := newAnalyzeCmd()
	if got := resolveAnalysisAddr(cmd, "localhost:50052"); got != "localhost:60000" {
		t.Fatalf("resolveAnalysisAddr without flag = %q, want config value", got)
	}
	if err := cmd.Flags().Set("analysis-addr", "localhost:70000"); err != nil {
		t.Fatal(err)
	}
	if got := resolveAnalysisAddr(cmd, "localhost:70000"); got != "localhost:70000" {
		t.Fatalf("resolveAnalysisAddr with flag = %q, want explicit flag value", got)
	}
}

type rootConfigState struct {
	cfgFile             string
	dbPath              string
	ibHost              string
	ibPortRaw           string
	defaultAnalysisAddr string
}

func restoreRootConfigState(s rootConfigState) {
	cfgFile = s.cfgFile
	dbPath = s.dbPath
	ibHost = s.ibHost
	ibPortRaw = s.ibPortRaw
	defaultAnalysisAddr = s.defaultAnalysisAddr
}

func writeRootConfigForTest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "optix.yaml")
	data := []byte(`
ibkr:
  host: "192.0.2.10"
  port: 7497
grpc:
  python_server_addr: "localhost:60000"
database:
  path: "/tmp/optix-config.db"
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
