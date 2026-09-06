package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDatabaseEnvironmentOverridesConfigButNotFlag(t *testing.T) {
	old := rootConfigState{cfgFile: cfgFile, dbPath: dbPath, ibHost: ibHost, ibPortRaw: ibPortRaw, defaultAnalysisAddr: defaultAnalysisAddr}
	defer restoreRootConfigState(old)
	p := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(p, []byte("database:\n  path: yaml.db\n"), 0600)
	cfgFile = p
	t.Setenv("OPTIX_DB_PATH", "env.db")
	if err := applyRootConfig(map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if dbPath != "env.db" {
		t.Fatalf("environment ignored: %s", dbPath)
	}
	dbPath = "flag.db"
	if err := applyRootConfig(map[string]bool{"db": true}); err != nil {
		t.Fatal(err)
	}
	if dbPath != "flag.db" {
		t.Fatal(dbPath)
	}
}

func TestRetainedLegacyPathPreventsFreshShellEmptyDatabase(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(t.TempDir(), "old.db")
	os.WriteFile(old, []byte("retained"), 0600)
	os.WriteFile(filepath.Join(root, "legacy-databases"), []byte(old+"\n"), 0600)
	paths, err := runtimeLegacyDatabases(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[1] != old {
		t.Fatalf("retained legacy location lost: %v", paths)
	}
}
