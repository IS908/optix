package cli

import (
	"strings"
	"testing"

	"github.com/IS908/optix/pkg/model"
)

// TestOptionQuoteFailureExitDistinguishesContractValidationFromNoData pins
// #193 finding 6(a): a genuine IB per-request error (e.g. errCode 200 "no
// security definition" for a bad strike/expiry) must exit with a distinct
// code from a plain "no usable price data" result, so scanner-style callers
// can branch on exit code alone instead of string-matching stderr. It also
// pins finding 1's other half: the real IB error text must be the headline
// message, not buried only inside the warnings CSV.
func TestOptionQuoteFailureExitDistinguishesContractValidationFromNoData(t *testing.T) {
	t.Run("ibkr per-request error uses exitNoData with the real IB reason", func(t *testing.T) {
		q := &model.OptionQuote{
			Warnings: []string{"ibkr_error: IB error 200: No security definition has been found for the request"},
		}
		err := optionQuoteFailureExit(q)
		if got := AsExitCode(err); got != exitNoData {
			t.Fatalf("AsExitCode = %d, want exitNoData (%d); err=%v", got, exitNoData, err)
		}
		if !strings.Contains(err.Error(), "IB error 200: No security definition has been found for the request") {
			t.Fatalf("error = %q, want it to surface the real IB error text", err)
		}
	})

	t.Run("plain no-data result keeps exitIBKRUnreachable and the full warnings list", func(t *testing.T) {
		q := &model.OptionQuote{} // no bid/ask/last/mark, no ibkr_error warning
		err := optionQuoteFailureExit(q)
		if got := AsExitCode(err); got != exitIBKRUnreachable {
			t.Fatalf("AsExitCode = %d, want exitIBKRUnreachable (%d); err=%v", got, exitIBKRUnreachable, err)
		}
		if !strings.Contains(err.Error(), "no usable price data") {
			t.Fatalf("error = %q, want the generic no-usable-price-data message", err)
		}
	})

	if exitNoData == exitIBKRUnreachable || exitNoData == exitOK || exitNoData == exitGenericErr || exitNoData == exitSQLiteErr {
		t.Fatalf("exitNoData (%d) collides with an existing documented exit code", exitNoData)
	}
}

func TestOptionQuoteHasPriceData(t *testing.T) {
	cases := []struct {
		name string
		q    *model.OptionQuote
		want bool
	}{
		{name: "nil", q: nil, want: false},
		{name: "empty", q: &model.OptionQuote{}, want: false},
		{name: "mark only", q: &model.OptionQuote{Mark: 1.23}, want: true},
		{name: "mid only", q: &model.OptionQuote{Mid: 1.23}, want: true},
		{name: "last only", q: &model.OptionQuote{Last: 1.23}, want: true},
		{name: "two-sided bid/ask", q: &model.OptionQuote{Bid: 1.20, Ask: 1.25}, want: true},
		{name: "one-sided bid only", q: &model.OptionQuote{Bid: 1.20}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := optionQuoteHasPriceData(tc.q); got != tc.want {
				t.Fatalf("optionQuoteHasPriceData = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestOptionQuoteCmdRegistersTimeoutFlag pins #193 finding 6(b): a
// scanner-style caller needs a hard subprocess budget, so `option-quote`
// must expose a --timeout flag. Default stays 0 (disabled) so existing
// callers keep today's ctx.Background()-plus-internal-timeout behavior
// unchanged.
func TestOptionQuoteCmdRegistersTimeoutFlag(t *testing.T) {
	cmd := newOptionQuoteCmd()
	f := cmd.Flags().Lookup("timeout")
	if f == nil {
		t.Fatal("expected --timeout flag to be registered")
	}
	if f.DefValue != "0s" {
		t.Fatalf("--timeout default = %q, want 0s (preserves current default behavior)", f.DefValue)
	}
}
