package ibkr

import (
	"testing"

	"github.com/scmhub/ibapi"
)

func TestWrapperPositionsAccumulator(t *testing.T) {
	w := newIbWrapper()
	pp := w.registerPositions()
	defer w.unregisterPositions()

	w.Position("U123",
		&ibapi.Contract{Symbol: "AAPL", SecType: "STK", Multiplier: ""},
		ibapi.StringToDecimal("100"), 245.30)
	w.Position("U123",
		&ibapi.Contract{Symbol: "AAPL", SecType: "OPT",
			LastTradeDateOrContractMonth: "20260515", Strike: 295, Right: "C", Multiplier: "100"},
		ibapi.StringToDecimal("2"), 310.00)
	w.PositionEnd()

	select {
	case <-pp.done:
	default:
		t.Fatal("PositionEnd did not close the done channel")
	}
	if len(pp.positions) != 2 {
		t.Fatalf("want 2 positions, got %d", len(pp.positions))
	}
	stk := pp.positions[0]
	if stk.Symbol != "AAPL" || stk.SecType != "STK" || stk.Quantity != 100 ||
		stk.AvgCost != 245.30 || stk.Multiplier != 1 {
		t.Errorf("stock position mismatch: %+v", stk)
	}
	opt := pp.positions[1]
	if opt.SecType != "OPT" || opt.Expiration != "20260515" || opt.Strike != 295 ||
		opt.Right != "C" || opt.Multiplier != 100 {
		t.Errorf("option position mismatch: %+v", opt)
	}
}

func TestWrapperExecutionsAccumulator(t *testing.T) {
	w := newIbWrapper()
	reqID := int64(42)
	pe := w.registerExecutions(reqID)
	defer w.unregister(reqID)

	w.ExecDetails(reqID,
		&ibapi.Contract{Symbol: "AAPL", SecType: "STK"},
		&ibapi.Execution{ExecID: "e1", Time: "20260510-14:32:00", AcctNumber: "U123",
			Side: "BOT", Shares: ibapi.StringToDecimal("100"), Price: 245.30,
			AvgPrice: 245.30, Exchange: "SMART", OrderID: 1, PermID: 1001})
	w.ExecDetails(reqID,
		&ibapi.Contract{Symbol: "TSLA", SecType: "STK"},
		&ibapi.Execution{ExecID: "e2", Time: "20260509-10:15:00", AcctNumber: "U123",
			Side: "SLD", Shares: ibapi.StringToDecimal("50"), Price: 380.00,
			AvgPrice: 380.00, Exchange: "SMART", OrderID: 2, PermID: 1002})
	w.ExecDetailsEnd(reqID)

	select {
	case <-pe.done:
	default:
		t.Fatal("ExecDetailsEnd did not close the done channel")
	}
	if len(pe.executions) != 2 {
		t.Fatalf("want 2 executions, got %d", len(pe.executions))
	}
	e1 := pe.executions[0]
	if e1.ExecID != "e1" || e1.Symbol != "AAPL" || e1.Side != "BOT" ||
		e1.Shares != 100 || e1.Price != 245.30 {
		t.Errorf("execution 0 mismatch: %+v", e1)
	}
	if e1.Time.IsZero() {
		t.Errorf("execution 0 Time failed to parse")
	}
}
