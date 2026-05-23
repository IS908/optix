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

// TestWrapperEndAfterErrorNoDoubleClose pins the invariant that an End
// callback firing AFTER an Error callback for the same reqID does not
// crash the process with "close of closed channel". See #28.
//
// IBKR's API can deliver an Error followed by the request's End
// callback (e.g. partial-data + error responses, or empty-result
// requests that still emit XxxEnd). Before the fix, the Error path
// closed `done` via `select { case <-done: default: close(done) }`
// while End paths called raw `close(done)` — so on the End-after-Error
// path the End would double-close and panic. The fix is to route every
// close through a per-pending sync.Once.
func TestWrapperEndAfterErrorNoDoubleClose(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(w *IbWrapper, reqID int64) <-chan struct{}
		fireEnd func(w *IbWrapper, reqID int64)
	}{
		{
			name: "executions",
			setup: func(w *IbWrapper, reqID int64) <-chan struct{} {
				pe := w.registerExecutions(reqID)
				_ = w.registerError(reqID)
				return pe.done
			},
			fireEnd: func(w *IbWrapper, reqID int64) { w.ExecDetailsEnd(reqID) },
		},
		{
			name: "bars",
			setup: func(w *IbWrapper, reqID int64) <-chan struct{} {
				pb := w.registerBars(reqID)
				_ = w.registerError(reqID)
				return pb.done
			},
			fireEnd: func(w *IbWrapper, reqID int64) { w.HistoricalDataEnd(reqID, "", "") },
		},
		{
			name: "contractDetails",
			setup: func(w *IbWrapper, reqID int64) <-chan struct{} {
				pcd := w.registerContractDetails(reqID)
				_ = w.registerError(reqID)
				return pcd.done
			},
			fireEnd: func(w *IbWrapper, reqID int64) { w.ContractDetailsEnd(reqID) },
		},
		{
			name: "optParams",
			setup: func(w *IbWrapper, reqID int64) <-chan struct{} {
				pp := w.registerOptParams(reqID)
				_ = w.registerError(reqID)
				return pp.done
			},
			fireEnd: func(w *IbWrapper, reqID int64) { w.SecurityDefinitionOptionParameterEnd(reqID) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("End after Error panicked: %v", r)
				}
			}()
			w := newIbWrapper()
			reqID := int64(42)
			done := tc.setup(w, reqID)
			defer w.unregister(reqID)

			// 1. Error fires first — closes done via the Error handler's
			//    data-channel-close branch (errCode 10168 = ≥2000 and ≠10167).
			w.Error(reqID, 0, 10168, "test error", "")
			select {
			case <-done:
				// expected: Error closed done
			default:
				t.Fatal("Error did not close the done channel")
			}

			// 2. End fires second. Pre-fix: raw close(done) on an
			//    already-closed channel → panic (caught by defer above).
			//    Post-fix: sync.Once swallows the second close.
			tc.fireEnd(w, reqID)
		})
	}
}
