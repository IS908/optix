package ibkr

import (
	"testing"

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
		&ibapi.Contract{Symbol: "AAPL", SecType: "STK"},
		ibapi.StringToDecimal("100"), 245.30)
	if stk.Symbol != "AAPL" || stk.SecType != "STK" || stk.Quantity != 100 ||
		stk.AvgCost != 245.30 || stk.Multiplier != 1 ||
		stk.Expiration != "" || stk.Strike != 0 || stk.Right != "" {
		t.Errorf("stock conversion mismatch: %+v", stk)
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
