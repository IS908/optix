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
