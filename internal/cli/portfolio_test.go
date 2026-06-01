package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewPortfolioConcentrationCmdFlags pins the dual-currency flag defaults
// so a future refactor can't silently drop them (the v0.5.0 regression was
// exactly that — the Report fields existed but no flag set them; see #51).
func TestNewPortfolioConcentrationCmdFlags(t *testing.T) {
	cmd := newPortfolioConcentrationCmd()
	for _, name := range []string{"net-liq-sgd", "fx-usd-sgd", "net-liq-usd"} {
		got, err := cmd.Flags().GetFloat64(name)
		if err != nil {
			t.Errorf("flag --%s not defined: %v", name, err)
			continue
		}
		if got != 0 {
			t.Errorf("default --%s = %v, want 0", name, got)
		}
	}
	if got, _ := cmd.Flags().GetString("portfolio-config"); got != "configs/portfolio.yaml" {
		t.Fatalf("default --portfolio-config = %q, want configs/portfolio.yaml", got)
	}
}

func TestResolveConcentrationSettingsReadsConfigAndAppliesFlagOverrides(t *testing.T) {
	path := writePortfolioConfigForCLITest(t)
	cfg, sectorsFile, err := resolveConcentrationSettings(path, "", 12.5, true, 0, false, 9, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WarnPct != 12.5 {
		t.Fatalf("WarnPct = %v, want flag override 12.5", cfg.WarnPct)
	}
	if cfg.RedPct != 33 {
		t.Fatalf("RedPct = %v, want config value 33", cfg.RedPct)
	}
	if cfg.TopN != 9 {
		t.Fatalf("TopN = %v, want flag override 9", cfg.TopN)
	}
	if sectorsFile != "/tmp/custom-sectors.json" {
		t.Fatalf("sectorsFile = %q, want config sectors file", sectorsFile)
	}

	cfg, sectorsFile, err = resolveConcentrationSettings(path, "/tmp/flag-sectors.json", 0, false, 0, false, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if sectorsFile != "/tmp/flag-sectors.json" {
		t.Fatalf("sectorsFile = %q, want flag sectors file", sectorsFile)
	}
	if cfg.WarnPct != 7 || cfg.TopN != 4 {
		t.Fatalf("cfg = %+v, want config concentration when threshold flags unset", cfg)
	}
}

func TestResolveConcentrationSettingsRejectsZeroFlagOverrides(t *testing.T) {
	path := writePortfolioConfigForCLITest(t)
	if _, _, err := resolveConcentrationSettings(path, "", 0, true, 0, false, 0, false); err == nil {
		t.Fatal("expected explicit --threshold-warn 0 to error")
	}
	if _, _, err := resolveConcentrationSettings(path, "", 0, false, 0, true, 0, false); err == nil {
		t.Fatal("expected explicit --threshold-red 0 to error")
	}
	if _, _, err := resolveConcentrationSettings(path, "", 0, false, 0, false, 0, true); err == nil {
		t.Fatal("expected explicit --top-n 0 to error")
	}
}

func TestValidateCurrencyFlags(t *testing.T) {
	cases := []struct {
		name       string
		netLiqSGD  float64
		fxUSDtoSGD float64
		wantOK     bool
	}{
		{"both unset", 0, 0, true},
		{"both set", 452929, 1.2765, true},
		{"sgd only", 452929, 0, false},
		{"fx only", 0, 1.2765, false},
		{"negative sgd", -5, 1.2765, false},
		{"negative fx", 452929, -1, false},
		{"both negative", -5, -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCurrencyFlags(tc.netLiqSGD, tc.fxUSDtoSGD)
			if (err == nil) != tc.wantOK {
				t.Errorf("validateCurrencyFlags(%g, %g) err=%v, wantOK=%v",
					tc.netLiqSGD, tc.fxUSDtoSGD, err, tc.wantOK)
			}
		})
	}
}

// TestValidateCurrencyFlagsNegativeMessage verifies a negative value produces
// the "must be positive" message rather than the misleading "must be passed
// together" — the user DID pass both, the problem is the sign.
func TestValidateCurrencyFlagsNegativeMessage(t *testing.T) {
	err := validateCurrencyFlags(-5, 1.2765)
	if err == nil {
		t.Fatal("expected error for negative net-liq-sgd")
	}
	if !strings.Contains(err.Error(), "must be positive") {
		t.Errorf("error should explain the sign problem, got: %v", err)
	}
}

func writePortfolioConfigForCLITest(t *testing.T) string {
	return writePortfolioConfigForCLITestWithRiskFreeRate(t, 0.052)
}

func writePortfolioConfigForCLITestWithRiskFreeRate(t *testing.T, riskFreeRate float64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "portfolio.yaml")
	data := []byte(fmt.Sprintf(`
concentration:
  warn_pct: 7
  red_pct: 33
  top_n: 4
  top2_warn_pct: 25
  top5_warn_pct: 55
  hhi_diversified_max: 1200
  hhi_concentrated_min: 2200
greeks:
  risk_free_rate: %g
sectors_file: "/tmp/custom-sectors.json"
stress:
  scenarios:
    - id: spy-down
      label: "SPY down"
      shocks:
        - axis: spy_pct
          magnitude: -0.03
`, riskFreeRate))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
