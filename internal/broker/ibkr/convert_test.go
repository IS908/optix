package ibkr

import (
	"testing"
	"time"

	"github.com/scmhub/ibapi"
)

func TestParseMultiplier(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		secType string
		want    float64
	}{
		{"option standard", "100", "OPT", 100},
		{"option empty defaults 100", "", "OPT", 100},
		{"option garbage defaults 100", "abc", "OPT", 100},
		{"stock empty defaults 1", "", "STK", 1},
		{"stock explicit 1", "1", "STK", 1},
		{"mini option", "10", "OPT", 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseMultiplier(tt.s, tt.secType); got != tt.want {
				t.Errorf("parseMultiplier(%q, %q) = %v, want %v", tt.s, tt.secType, got, tt.want)
			}
		})
	}
}

func TestPositionFromIB(t *testing.T) {
	stk := positionFromIB("U1",
		&ibapi.Contract{Symbol: "AAPL", SecType: "STK", Currency: "USD"},
		ibapi.StringToDecimal("100"), 245.30)
	if stk.Symbol != "AAPL" || stk.SecType != "STK" || stk.Quantity != 100 ||
		stk.AvgCost != 245.30 || stk.Multiplier != 1 || stk.Currency != "USD" ||
		stk.Expiration != "" || stk.Strike != 0 || stk.Right != "" {
		t.Errorf("stock conversion mismatch: %+v", stk)
	}

	// Non-USD contract currency must be carried through (issue #49).
	hk := positionFromIB("U1",
		&ibapi.Contract{Symbol: "0700", SecType: "STK", Currency: "HKD"},
		ibapi.StringToDecimal("100"), 380.00)
	if hk.Currency != "HKD" {
		t.Errorf("Currency not carried through: got %q, want HKD", hk.Currency)
	}

	opt := positionFromIB("U1",
		&ibapi.Contract{Symbol: "AAPL", SecType: "OPT",
			LastTradeDateOrContractMonth: "20260515", Strike: 295, Right: "C", Multiplier: "100"},
		ibapi.StringToDecimal("-2"), 310.00)
	if opt.SecType != "OPT" || opt.Expiration != "20260515" || opt.Strike != 295 ||
		opt.Right != "C" || opt.Quantity != -2 || opt.Multiplier != 100 {
		t.Errorf("option conversion mismatch: %+v", opt)
	}
}

func TestExecutionFromIB(t *testing.T) {
	ex := executionFromIB(
		&ibapi.Contract{Symbol: "AAPL", SecType: "STK"},
		&ibapi.Execution{ExecID: "e1", Time: "20260510-14:32:00", AcctNumber: "U1",
			Side: "BOT", Shares: ibapi.StringToDecimal("100"), Price: 245.30,
			AvgPrice: 245.31, Exchange: "SMART", OrderID: 7, PermID: 700})
	if ex.ExecID != "e1" || ex.Symbol != "AAPL" || ex.Side != "BOT" ||
		ex.Shares != 100 || ex.Price != 245.30 || ex.AvgPrice != 245.31 ||
		ex.Exchange != "SMART" || ex.OrderID != 7 || ex.PermID != 700 {
		t.Errorf("execution conversion mismatch: %+v", ex)
	}
	if ex.Time.IsZero() {
		t.Errorf("Time failed to parse from %q", "20260510-14:32:00")
	}
}

// TestParseExecTime covers the layouts IBKR has been observed to send via
// scmhub/ibapi, including the IANA-timezone-suffixed variant that started
// landing in newer API versions and which broke journal sync (see #27).
// Silent zero returns on parse failure had made the bug invisible —
// these cases lock the parser's contract so the next format change shows up
// in tests, not in days of bogus 0001-01-01 rows.
func TestParseExecTime(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantZero bool
		wantTZ   string // IANA name; empty = don't assert tz
	}{
		{
			name:   "IANA timezone suffix (America/New_York)",
			in:     "20260520 14:30:21 America/New_York",
			wantTZ: "America/New_York",
		},
		{
			name:   "IANA timezone suffix (Asia/Singapore)",
			in:     "20260520 14:30:21 Asia/Singapore",
			wantTZ: "Asia/Singapore",
		},
		{
			name: "dash separator (legacy / paper)",
			in:   "20260510-14:32:00",
		},
		{
			name: "single space separator",
			in:   "20260510 14:32:00",
		},
		{
			name:     "empty string",
			in:       "",
			wantZero: true,
		},
		{
			name:     "garbage",
			in:       "not-a-time",
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseExecTime(tt.in)
			if tt.wantZero {
				if !got.IsZero() {
					t.Errorf("parseExecTime(%q) = %v, want zero time", tt.in, got)
				}
				return
			}
			if got.IsZero() {
				t.Errorf("parseExecTime(%q) returned zero time; expected a parsed time", tt.in)
				return
			}
			if tt.wantTZ != "" {
				loc, err := time.LoadLocation(tt.wantTZ)
				if err != nil {
					t.Fatalf("LoadLocation(%q): %v", tt.wantTZ, err)
				}
				// Compare by wall-clock-in-tz: the parsed instant, rendered
				// in the expected tz, should match the source HH:MM:SS.
				if got.In(loc).Format("20060102 15:04:05") != "20260520 14:30:21" {
					t.Errorf("parseExecTime(%q) = %v (in %s: %v), want wall time 20260520 14:30:21 in %s",
						tt.in, got, tt.wantTZ, got.In(loc), tt.wantTZ)
				}
			}
		})
	}
}
