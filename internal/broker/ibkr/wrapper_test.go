package ibkr

import (
	"sync"
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

func TestWrapperQuoteAccumulatorConcurrentSnapshotRace(t *testing.T) {
	w := newIbWrapper()
	reqID := int64(1001)
	pq := w.registerQuote(reqID)
	defer w.unregister(reqID)

	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			price := float64(i + 1)
			w.TickPrice(reqID, ibapi.BID, price, ibapi.TickAttrib{})
			w.TickPrice(reqID, ibapi.ASK, price+0.25, ibapi.TickAttrib{})
			w.TickPrice(reqID, ibapi.LAST, price+0.10, ibapi.TickAttrib{})
			w.TickSize(reqID, ibapi.VOLUME, ibapi.StringToDecimal("100"))
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			snap := pq.snapshot()
			_ = snap.bid + snap.ask + snap.last + snap.volume
		}
	}()

	close(start)
	wg.Wait()
}

func TestWrapperQuoteAccumulatorCapturesDelayedPrices(t *testing.T) {
	w := newIbWrapper()
	reqID := int64(1003)
	pq := w.registerQuote(reqID)
	defer w.unregister(reqID)

	w.TickPrice(reqID, ibapi.DELAYED_BID, 99.50, ibapi.TickAttrib{})
	w.TickPrice(reqID, ibapi.DELAYED_ASK, 100.50, ibapi.TickAttrib{})
	w.TickPrice(reqID, ibapi.DELAYED_LAST, 101.25, ibapi.TickAttrib{})
	w.TickPrice(reqID, ibapi.DELAYED_CLOSE, 100.00, ibapi.TickAttrib{})
	w.TickPrice(reqID, ibapi.MARK_PRICE, 102.00, ibapi.TickAttrib{})
	w.TickPrice(reqID, ibapi.DELAYED_LAST, 101.75, ibapi.TickAttrib{})
	w.TickSize(reqID, ibapi.DELAYED_VOLUME, ibapi.StringToDecimal("12345"))

	snap := pq.snapshot()
	if snap.bid != 99.50 || snap.ask != 100.50 || snap.mark != 102.00 || snap.last != 101.75 || snap.close != 100.00 {
		t.Fatalf("delayed price snapshot mismatch: %+v", snap)
	}
	if snap.volume != 12345 {
		t.Fatalf("delayed volume = %v, want 12345", snap.volume)
	}
}

func TestWrapperOptionQuoteAccumulatorCapturesValidationTicks(t *testing.T) {
	w := newIbWrapper()
	reqID := int64(1005)
	pq := w.registerQuote(reqID)
	defer w.unregister(reqID)

	w.MarketDataType(reqID, int64(ibapi.REALTIME))
	w.TickPrice(reqID, ibapi.BID, 1.20, ibapi.TickAttrib{})
	w.TickPrice(reqID, ibapi.ASK, 1.25, ibapi.TickAttrib{})
	w.TickPrice(reqID, ibapi.LAST, 1.23, ibapi.TickAttrib{})
	w.TickSize(reqID, ibapi.OPTION_PUT_VOLUME, ibapi.StringToDecimal("1234"))
	w.TickSize(reqID, ibapi.OPTION_PUT_OPEN_INTEREST, ibapi.StringToDecimal("5678"))
	w.TickOptionComputation(reqID, ibapi.MODEL_OPTION, 0, 0.31, -0.24, 1.23, 0, 0.01, 0.12, -0.04, 290)

	snap := pq.snapshot()
	if snap.marketDataType != "real_time" {
		t.Fatalf("marketDataType = %q, want real_time", snap.marketDataType)
	}
	if snap.bid != 1.20 || snap.ask != 1.25 || snap.last != 1.23 || snap.mark != 1.23 {
		t.Fatalf("price snapshot mismatch: %+v", snap)
	}
	if snap.volume != 1234 || snap.openInterest != 5678 {
		t.Fatalf("size snapshot mismatch: %+v", snap)
	}
	if snap.impliedVolatility != 0.31 || snap.greeks.Delta != -0.24 || snap.greeks.Gamma != 0.01 ||
		snap.greeks.Vega != 0.12 || snap.greeks.Theta != -0.04 {
		t.Fatalf("option computation mismatch: %+v", snap)
	}
}

func TestWrapperMarketDepthAccumulatorCapturesBidAskLevels(t *testing.T) {
	w := newIbWrapper()
	reqID := int64(1004)
	pd := w.registerDepth(reqID, 5)
	defer w.unregister(reqID)

	w.UpdateMktDepth(reqID, 0, 0, 1, 499.90, ibapi.StringToDecimal("1000"))
	w.UpdateMktDepth(reqID, 1, 0, 1, 499.80, ibapi.StringToDecimal("800"))
	w.UpdateMktDepth(reqID, 0, 0, 0, 500.10, ibapi.StringToDecimal("900"))
	w.UpdateMktDepth(reqID, 1, 0, 0, 500.20, ibapi.StringToDecimal("850"))

	levels := pd.snapshot()
	if len(levels) != 4 {
		t.Fatalf("levels = %d, want 4: %#v", len(levels), levels)
	}
	if levels[0].Side != "bid" || levels[0].Position != 0 || levels[0].Price != 499.90 || levels[0].Size != 1000 {
		t.Fatalf("bid level 0 mismatch: %#v", levels[0])
	}
	if levels[2].Side != "ask" || levels[2].Position != 0 || levels[2].Price != 500.10 || levels[2].Size != 900 {
		t.Fatalf("ask level 0 mismatch: %#v", levels[2])
	}
}

func TestQuoteChangeSupportsNegativeAndMidpoint(t *testing.T) {
	change, pct := quoteChange(95, 100)
	if change != -5 {
		t.Fatalf("change = %v, want -5", change)
	}
	if pct != -5 {
		t.Fatalf("pct = %v, want -5", pct)
	}

	mid := (99.50 + 100.50) / 2
	change, pct = quoteChange(mid, 98)
	if change != 2 {
		t.Fatalf("midpoint change = %v, want 2", change)
	}
	if pct < 2.0408 || pct > 2.0409 {
		t.Fatalf("midpoint pct = %v, want about 2.0408", pct)
	}
}

func TestWrapperOITickAccumulatorConcurrentSnapshotRace(t *testing.T) {
	w := newIbWrapper()
	reqID := int64(1002)
	po := w.registerOI(reqID)
	defer w.unregister(reqID)

	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			w.TickSize(reqID, 86, ibapi.StringToDecimal("200"))
			w.TickOptionComputation(reqID, 0, 0, 0.42, 0, 0, 0, 0, 0, 0, 0)
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			snap := po.snapshot()
			_ = snap.openInterest + int32(snap.iv)
		}
	}()

	close(start)
	wg.Wait()
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
