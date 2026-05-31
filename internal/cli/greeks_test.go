package cli

import "testing"

func TestNewPortfolioGreeksCmdFlags(t *testing.T) {
	cmd := newPortfolioGreeksCmd()
	if got, _ := cmd.Flags().GetString("by"); got != "underlying" {
		t.Errorf("default --by = %q, want underlying", got)
	}
	for _, name := range []string{"net-liq-usd", "risk-free-rate", "sectors-file", "json"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not defined", name)
		}
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
