package portfolio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigParsesPortfolioYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portfolio.yaml")
	if err := os.WriteFile(path, []byte(`
concentration:
  warn_pct: 12.5
  red_pct: 24.0
  top_n: 7
greeks:
  risk_free_rate: 0.051
iv_staleness:
  fresh_max: "2h"
  acceptable_max: "12h"
  stale_max: "72h"
sectors_file: "./custom-sectors.json"
stress:
  scenarios:
    - id: spy-down-3
      label: "SPY -3%"
      shocks:
        - axis: spy_pct
          magnitude: -0.03
    - id: tech-correlated
      label: "Tech correlated"
      shocks:
        - axis: spy_pct
          magnitude: -0.05
        - axis: iv_points
          magnitude: 3.0
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Concentration.WarnPct != 12.5 || cfg.Concentration.RedPct != 24.0 || cfg.Concentration.TopN != 7 {
		t.Fatalf("concentration = %+v", cfg.Concentration)
	}
	if cfg.Greeks.RiskFreeRate != 0.051 {
		t.Fatalf("risk_free_rate = %v", cfg.Greeks.RiskFreeRate)
	}
	if cfg.IVStaleness.FreshMax != 2*time.Hour || cfg.IVStaleness.AcceptableMax != 12*time.Hour || cfg.IVStaleness.StaleMax != 72*time.Hour {
		t.Fatalf("iv staleness = %+v", cfg.IVStaleness)
	}
	if cfg.SectorsFile != "./custom-sectors.json" {
		t.Fatalf("sectors_file = %q", cfg.SectorsFile)
	}
	if len(cfg.Stress.Scenarios) != 2 {
		t.Fatalf("scenarios = %+v", cfg.Stress.Scenarios)
	}
	if cfg.Stress.Scenarios[1].Shocks[1].Axis != "iv_points" || cfg.Stress.Scenarios[1].Shocks[1].Magnitude != 3.0 {
		t.Fatalf("second scenario shocks = %+v", cfg.Stress.Scenarios[1].Shocks)
	}
}

func TestLoadConfigRejectsUnknownStressAxis(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portfolio.yaml")
	if err := os.WriteFile(path, []byte(`
stress:
  scenarios:
    - id: typo
      label: "Typo"
      shocks:
        - axis: spy_percent
          magnitude: -0.03
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected unsupported axis error")
	}
}

func TestLoadConfigRejectsUnknownTopLevelSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portfolio.yaml")
	if err := os.WriteFile(path, []byte(`
stres:
  scenarios:
    - id: typo
      label: "Typo"
      shocks:
        - axis: spy_pct
          magnitude: -0.03
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected unknown top-level section error")
	}
}

func TestLoadConfigRejectsUnknownStressKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portfolio.yaml")
	if err := os.WriteFile(path, []byte(`
stress:
  scenarioz:
    - id: typo
      label: "Typo"
      shocks:
        - axis: spy_pct
          magnitude: -0.03
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected unknown stress key error")
	}
}

func TestLoadConfigRejectsDuplicateStressScenarioID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portfolio.yaml")
	if err := os.WriteFile(path, []byte(`
stress:
  scenarios:
    - id: duplicate
      label: "First"
      shocks:
        - axis: spy_pct
          magnitude: -0.03
    - id: duplicate
      label: "Second"
      shocks:
        - axis: iv_points
          magnitude: 3
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), `duplicate stress scenario id "duplicate"`) {
		t.Fatalf("expected duplicate scenario id error, got %v", err)
	}
}

func TestLoadConfigRejectsDuplicateTopLevelSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portfolio.yaml")
	if err := os.WriteFile(path, []byte(`
greeks:
  risk_free_rate: 0.04
greeks:
  risk_free_rate: 0.05
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), `duplicate top-level portfolio config section "greeks"`) {
		t.Fatalf("expected duplicate section error, got %v", err)
	}
}

func TestLoadConfigRejectsTabIndentWithClearError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portfolio.yaml")
	if err := os.WriteFile(path, []byte("concentration:\n\twarn_pct: 12.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "tabs are not allowed for indentation") {
		t.Fatalf("expected tab indentation error, got %v", err)
	}
}

func TestLoadConfigMissingFileUsesDefaults(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Concentration.WarnPct != DefaultConfig().WarnPct || cfg.Concentration.TopN != DefaultConfig().TopN {
		t.Fatalf("default concentration = %+v", cfg.Concentration)
	}
	if cfg.Greeks.RiskFreeRate != 0.043 {
		t.Fatalf("default risk_free_rate = %v", cfg.Greeks.RiskFreeRate)
	}
	if len(cfg.Stress.Scenarios) != 6 {
		t.Fatalf("default stress scenarios = %d, want 6", len(cfg.Stress.Scenarios))
	}
}
