package portfolio

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedSectorsMatchesCanonical guards against drift between the
// canonical configs/sectors.json (the human-editable source) and the
// committed copy in internal/portfolio/default_sectors.json (the go:embed
// target). go:embed can't traverse `..` or follow symlinks, so the copy
// has to be maintained manually — this test makes a divergence loud
// rather than silent.
func TestEmbeddedSectorsMatchesCanonical(t *testing.T) {
	canonical, err := os.ReadFile("../../configs/sectors.json")
	if err != nil {
		t.Fatalf("read canonical configs/sectors.json: %v", err)
	}
	if !bytes.Equal(canonical, defaultSectorsJSON) {
		t.Errorf("configs/sectors.json and internal/portfolio/default_sectors.json have drifted.\n"+
			"Run: cp configs/sectors.json internal/portfolio/default_sectors.json\n"+
			"canonical=%d bytes  embedded=%d bytes",
			len(canonical), len(defaultSectorsJSON))
	}
}

// TestEmbeddedDefaultSectorMapParses guards against shipping a binary with
// a malformed embedded sectors.json. The embed source is a committed copy
// of configs/sectors.json (go:embed can't traverse `..` or follow symlinks).
// See TestEmbeddedSectorsMatchesCanonical for drift detection between the
// two copies. If someone breaks the JSON, every downstream consumer that
// falls back to the embedded map gets a runtime error in production.
func TestEmbeddedDefaultSectorMapParses(t *testing.T) {
	sm, err := LoadDefaultSectorMap()
	if err != nil {
		t.Fatalf("embedded default sectors.json failed to parse: %v", err)
	}
	if len(sm.TickerSectors) == 0 {
		t.Errorf("embedded default sectors should map at least one ticker, got 0")
	}
	if len(sm.SectorLabels) == 0 {
		t.Errorf("embedded default sectors should have at least one sector label, got 0")
	}
	// Smoke check: a known ticker (AAPL is in the shipped map) resolves to a
	// non-empty sector with a non-empty label.
	if got := sm.Sector("AAPL"); got == "unclassified" || got == "" {
		t.Errorf("AAPL should resolve to a real sector in the shipped map, got %q", got)
	}
}

// TestResolveSectorMapEmbeddedFallback guards the headline behavior of fix
// #48: from a cwd with no ./configs/, ResolveSectorMap must return the
// embedded map, not an empty one. The v0.5.0 default `--sectors-file=
// ./configs/sectors.json` silently collapsed every ticker to "unclassified"
// in this scenario.
func TestResolveSectorMapEmbeddedFallback(t *testing.T) {
	// Run from a temp dir with no configs/ to simulate a cron/install cwd.
	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	// Don't let an inherited env override the test.
	t.Setenv("OPTIX_SECTORS_FILE", "")

	sm, source, err := ResolveSectorMap("")
	if err != nil {
		t.Fatalf("ResolveSectorMap with no on-disk override should succeed via embedded fallback, got %v", err)
	}
	if source != "<embedded>" {
		t.Errorf("expected source=<embedded>, got %q", source)
	}
	if got := sm.Sector("AAPL"); got == "unclassified" {
		t.Errorf("embedded map should classify AAPL, got 'unclassified'")
	}
}

// TestResolveSectorMapExplicitOverride confirms an explicit --sectors-file
// path wins over the embedded fallback, and a missing override fails loudly
// rather than silently using the embedded copy.
func TestResolveSectorMapExplicitOverride(t *testing.T) {
	tmp := t.TempDir()
	overridePath := filepath.Join(tmp, "my_sectors.json")
	custom := `{"sector_labels":{"x":"X"},"ticker_sectors":{"FOO":"x"}}`
	if err := os.WriteFile(overridePath, []byte(custom), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}

	sm, source, err := ResolveSectorMap(overridePath)
	if err != nil {
		t.Fatalf("explicit override should load: %v", err)
	}
	if source != overridePath {
		t.Errorf("source should be the explicit path, got %q", source)
	}
	if got := sm.Sector("FOO"); got != "x" {
		t.Errorf("FOO should map to 'x' from the override, got %q", got)
	}
	// AAPL is not in the override, so it should resolve to unclassified
	// (NOT fall back to embedded — the override is authoritative).
	if got := sm.Sector("AAPL"); got != "unclassified" {
		t.Errorf("AAPL not in override should be unclassified, got %q", got)
	}

	// Missing explicit path must fail loudly, not silently fall back.
	_, _, err = ResolveSectorMap(filepath.Join(tmp, "does-not-exist.json"))
	if err == nil {
		t.Errorf("missing explicit --sectors-file should error, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "load --sectors-file") {
		t.Errorf("error should mention --sectors-file, got: %v", err)
	}
}

// TestResolveSectorMapEnvOverride confirms $OPTIX_SECTORS_FILE is honored
// when --sectors-file is empty.
func TestResolveSectorMapEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, "env_sectors.json")
	custom := `{"sector_labels":{"e":"Env"},"ticker_sectors":{"ENV":"e"}}`
	if err := os.WriteFile(envPath, []byte(custom), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	t.Setenv("OPTIX_SECTORS_FILE", envPath)

	sm, source, err := ResolveSectorMap("")
	if err != nil {
		t.Fatalf("env override should load: %v", err)
	}
	if source != envPath {
		t.Errorf("source should be $OPTIX_SECTORS_FILE path, got %q", source)
	}
	if got := sm.Sector("ENV"); got != "e" {
		t.Errorf("ENV should map to 'e' from env override, got %q", got)
	}
}

// TestResolveSectorMapEnvTypoFailsLoudly guards the post-PR-#53-review
// change: $OPTIX_SECTORS_FILE is user intent (same class as the explicit
// flag), so a missing or unreadable env path must error rather than
// silently degrading to the embedded fallback. Without this, a user who
// exports the env var with a typo loses their customizations exactly the
// way issue #48 did, just one tier down.
func TestResolveSectorMapEnvTypoFailsLoudly(t *testing.T) {
	tmp := t.TempDir()
	missingPath := filepath.Join(tmp, "this-file-does-not-exist.json")
	t.Setenv("OPTIX_SECTORS_FILE", missingPath)

	_, _, err := ResolveSectorMap("")
	if err == nil {
		t.Fatalf("missing $OPTIX_SECTORS_FILE path should error, got nil")
	}
	if !strings.Contains(err.Error(), "OPTIX_SECTORS_FILE") {
		t.Errorf("error should mention the env var name, got: %v", err)
	}
}
