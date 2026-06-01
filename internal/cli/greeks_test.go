package cli

import "testing"

func TestNewPortfolioGreeksCmdFlags(t *testing.T) {
	cmd := newPortfolioGreeksCmd()
	if got, _ := cmd.Flags().GetString("by"); got != "underlying" {
		t.Errorf("default --by = %q, want underlying", got)
	}
	for _, name := range []string{"net-liq-usd", "risk-free-rate", "sectors-file", "portfolio-config", "json"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not defined", name)
		}
	}
	if got, _ := cmd.Flags().GetString("portfolio-config"); got != "configs/portfolio.yaml" {
		t.Fatalf("default --portfolio-config = %q, want configs/portfolio.yaml", got)
	}
}

func TestResolveGreeksSettingsReadsConfigAndAppliesFlagOverrides(t *testing.T) {
	path := writePortfolioConfigForCLITest(t)
	riskFreeRate, sectorsFile, err := resolveGreeksSettings(path, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if riskFreeRate != 0.052 {
		t.Fatalf("riskFreeRate = %v, want config value 0.052", riskFreeRate)
	}
	if sectorsFile != "/tmp/custom-sectors.json" {
		t.Fatalf("sectorsFile = %q, want config sectors file", sectorsFile)
	}

	riskFreeRate, sectorsFile, err = resolveGreeksSettings(path, "/tmp/flag-sectors.json", 0.061, true)
	if err != nil {
		t.Fatal(err)
	}
	if riskFreeRate != 0.061 {
		t.Fatalf("riskFreeRate = %v, want flag override 0.061", riskFreeRate)
	}
	if sectorsFile != "/tmp/flag-sectors.json" {
		t.Fatalf("sectorsFile = %q, want flag sectors file", sectorsFile)
	}

	riskFreeRate, _, err = resolveGreeksSettings(path, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if riskFreeRate != 0 {
		t.Fatalf("riskFreeRate = %v, want explicit flag value 0", riskFreeRate)
	}
}

func TestResolveGreeksSettingsPreservesZeroFromConfig(t *testing.T) {
	path := writePortfolioConfigForCLITestWithRiskFreeRate(t, 0)
	riskFreeRate, _, err := resolveGreeksSettings(path, "", 99, false)
	if err != nil {
		t.Fatal(err)
	}
	if riskFreeRate != 0 {
		t.Fatalf("riskFreeRate = %v, want YAML value 0", riskFreeRate)
	}
}

func TestValidateGroupBy(t *testing.T) {
	for _, ok := range []string{"underlying", "sector"} {
		if err := validateGroupBy(ok); err != nil {
			t.Errorf("validateGroupBy(%q) errored: %v", ok, err)
		}
	}
	if err := validateGroupBy("bogus"); err == nil {
		t.Errorf("validateGroupBy(bogus) should error")
	}
}
