package cli

import (
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
}

func TestValidateCurrencyFlags(t *testing.T) {
	cases := []struct {
		name      string
		netLiqSGD float64
		fxUSDtoSGD float64
		wantOK    bool
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
