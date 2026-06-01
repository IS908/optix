package cli

import "testing"

func TestNewPortfolioStressCmdFlags(t *testing.T) {
	cmd := newPortfolioStressCmd()
	if got, _ := cmd.Flags().GetString("portfolio-config"); got != "configs/portfolio.yaml" {
		t.Fatalf("default --portfolio-config = %q, want configs/portfolio.yaml", got)
	}
	if got, _ := cmd.Flags().GetString("analysis-addr"); got != "localhost:50052" {
		t.Fatalf("default --analysis-addr = %q", got)
	}
	if cmd.Flags().Lookup("json") == nil || cmd.Flags().Lookup("net-liq-usd") == nil {
		t.Fatalf("expected --json and --net-liq-usd flags")
	}
}

func TestResolveStressSettingsPreservesZeroRiskFreeRate(t *testing.T) {
	path := writePortfolioConfigForCLITestWithRiskFreeRate(t, 0)
	_, riskFreeRate, sectorsFile, err := resolveStressSettings(path, "", 99, false)
	if err != nil {
		t.Fatal(err)
	}
	if riskFreeRate != 0 {
		t.Fatalf("riskFreeRate = %v, want YAML value 0", riskFreeRate)
	}
	if sectorsFile != "/tmp/custom-sectors.json" {
		t.Fatalf("sectorsFile = %q, want config sectors file", sectorsFile)
	}

	_, riskFreeRate, sectorsFile, err = resolveStressSettings(path, "/tmp/flag-sectors.json", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if riskFreeRate != 0 {
		t.Fatalf("riskFreeRate = %v, want explicit flag value 0", riskFreeRate)
	}
	if sectorsFile != "/tmp/flag-sectors.json" {
		t.Fatalf("sectorsFile = %q, want flag sectors file", sectorsFile)
	}
}
