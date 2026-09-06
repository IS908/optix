package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/IS908/optix/internal/datahome"
	"github.com/spf13/cobra"
)

func legacyDatabases() ([]string, error) {
	paths := []string{filepath.Join("data", "optix.db")}
	if exe, err := os.Executable(); err == nil {
		if real, e := filepath.EvalSymlinks(exe); e == nil {
			exe = real
		}
		extra, err := runtimeLegacyDatabases(filepath.Dir(filepath.Dir(exe)))
		if err != nil {
			return nil, err
		}
		paths = append(paths, extra...)
	}
	return paths, nil
}

func runtimeLegacyDatabases(root string) ([]string, error) {
	paths := []string{filepath.Join(root, "data", "optix.db")}
	data, err := os.ReadFile(filepath.Join(root, "legacy-databases"))
	if os.IsNotExist(err) {
		return paths, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read retained database locations: %w", err)
	}
	for _, p := range strings.Split(string(data), "\n") {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("invalid retained database location: %q", p)
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func resolveDatabase(changed map[string]bool, config string, guard bool) (datahome.Resolution, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	o := datahome.Options{Home: home, OS: runtime.GOOS, XDG: os.Getenv("XDG_DATA_HOME"), Env: os.Getenv("OPTIX_DB_PATH"), Config: config}
	if changed["db"] {
		o.Flag = dbPath
		if o.Flag == "" {
			return datahome.Resolution{}, fmt.Errorf("--db cannot be empty")
		}
	}
	if guard && o.Flag == "" && o.Env == "" && o.Config == "" {
		o.Legacy, err = legacyDatabases()
		if err != nil {
			return datahome.Resolution{}, err
		}
	}
	return datahome.Resolve(o)
}

func newDataCmd() *cobra.Command {
	// Recovery/diagnosis must remain reachable even when the default location
	// guard would reject a normal command because a legacy database exists.
	cmd := &cobra.Command{Use: "data", Short: "Diagnose storage paths and safely copy a legacy database", PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	cmd.AddCommand(&cobra.Command{Use: "status", Args: cobra.NoArgs, Short: "Show resolved database path and legacy locations (no database opened)", RunE: func(c *cobra.Command, _ []string) error {
		cfg, err := loadRootConfig(cfgFile)
		if err != nil {
			return err
		}
		r, err := resolveDatabase(changedFlags(c), cfg.DBPath, false)
		if err != nil {
			return err
		}
		_, guardErr := resolveDatabase(changedFlags(c), cfg.DBPath, true)
		exe, _ := os.Executable()
		exe, _ = filepath.EvalSymlinks(exe)
		mode := "standalone"
		runtimeRoot := filepath.Dir(filepath.Dir(exe))
		if _, e := os.Stat(filepath.Join(runtimeRoot, "Makefile")); e == nil {
			mode = "dev"
		}
		abs, err := filepath.Abs(r.Path)
		if err != nil {
			return err
		}
		r.Path = abs
		warning := ""
		if guardErr != nil {
			warning = guardErr.Error()
		}
		return json.NewEncoder(c.OutOrStdout()).Encode(struct {
			Version  string              `json:"version"`
			Runtime  string              `json:"runtime"`
			Mode     string              `json:"mode"`
			Database datahome.Resolution `json:"database"`
			Warning  string              `json:"warning,omitempty"`
		}{version, runtimeRoot, mode, r, warning})
	}})
	var from, to string
	migrate := &cobra.Command{Use: "migrate", Args: cobra.NoArgs, Short: "Copy a consistent SQLite snapshot; retain source and recovery backup", Long: "Copy committed SQLite data, including WAL, without overwriting a target. Stop all writers before switching to the new database; later source writes are not synchronized. This command never changes your configuration.", RunE: func(c *cobra.Command, _ []string) error {
		backup, err := datahome.Migrate(c.Context(), from, to)
		if err != nil {
			return err
		}
		return json.NewEncoder(c.OutOrStdout()).Encode(map[string]string{"source": from, "destination": to, "backup": backup, "next": "Stop old writers, then explicitly select the destination via --db, OPTIX_DB_PATH or YAML. Source retained."})
	}}
	migrate.Flags().StringVar(&from, "from", "", "existing source database")
	migrate.Flags().StringVar(&to, "to", "", "new destination (must not exist)")
	_ = migrate.MarkFlagRequired("from")
	_ = migrate.MarkFlagRequired("to")
	cmd.AddCommand(migrate)
	return cmd
}
