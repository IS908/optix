package ibkr

import (
	"fmt"
	"strings"
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
	pq := w.registerQuote(reqID, "")
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
	pq := w.registerQuote(reqID, "")
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
	pq := w.registerQuote(reqID, "P")
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

// --- #193 finding 1: whitelisted sub-2000 IB error codes must reach errCh,
// but ONLY for reqIDs that opted into strict routing (review follow-up:
// the original unconditional whitelist broke GetMarketDepth's partial-rows
// contract and GetQuote's no-subscription history fallback — see
// strictErrorCodes' doc comment in wrapper.go) -------------------------------

func TestWrapperErrorRoutesStrictCodesOnStrictReqID(t *testing.T) {
	// Pre-#193, ALL errCode < 2000 were treated as informational noise and
	// dropped — including codes that mean "this specific request failed",
	// e.g. 200 "No security definition has been found for the request" (bad
	// strike/expiry/right). That silently starved the caller until its
	// timeout instead of failing fast with the real reason (finding 1).
	// GetOptionQuoteDetails is the only caller that registers strict, and
	// on a strict reqID these codes must still reach errCh — this existing
	// behavior must stay green through the per-reqID scoping fix.
	for _, code := range []int64{200, 162, 300, 321} {
		t.Run(fmt.Sprintf("code_%d", code), func(t *testing.T) {
			w := newIbWrapper()
			reqID := int64(2000 + code)
			pq := w.registerQuote(reqID, "P")
			errCh := w.registerStrictError(reqID)
			defer w.unregister(reqID)

			w.Error(reqID, 0, code, "synthetic test error", "")

			select {
			case err := <-errCh:
				if !strings.Contains(err.Error(), fmt.Sprintf("%d", code)) {
					t.Fatalf("errCh error = %q, want it to mention code %d", err, code)
				}
			default:
				t.Fatalf("code %d did not reach errCh on a strict reqID — still swallowed as informational", code)
			}
			select {
			case <-pq.done:
			default:
				t.Fatalf("code %d did not close pq.done on a strict reqID — caller would still wait out the full timeout", code)
			}
		})
	}
}

func TestWrapperErrorDoesNotRouteStrictCodesOnNonStrictReqID(t *testing.T) {
	// The review High: the whitelist previously fired for EVERY reqID-keyed
	// request, not just option-quote's, because strict routing didn't exist
	// as a concept — every registerError caller got the same fail-fast
	// behavior. GetQuote, GetMarketDepth, GetHistoricalBars, GetOptionChain,
	// fetchOIForContract, and GetExecutions all register via plain
	// registerError (non-strict) and must get the pre-#193 tolerant
	// behavior back: whitelisted codes dropped as informational, exactly
	// like every other sub-2000 code. This must FAIL against the
	// unconditionally-whitelisted code (i.e. before per-reqID scoping).
	for _, code := range []int64{200, 162, 300, 321} {
		t.Run(fmt.Sprintf("code_%d", code), func(t *testing.T) {
			w := newIbWrapper()
			reqID := int64(6000 + code)
			pq := w.registerQuote(reqID, "P")
			errCh := w.registerError(reqID) // non-strict
			defer w.unregister(reqID)

			w.Error(reqID, 0, code, "synthetic test error", "")

			select {
			case err := <-errCh:
				t.Fatalf("code %d reached errCh on a NON-strict reqID: %v — pre-#193 tolerance not restored", code, err)
			default:
			}
			select {
			case <-pq.done:
				t.Fatalf("code %d closed pq.done on a NON-strict reqID — only strict reqIDs may fast-fail", code)
			default:
			}
		})
	}
}

func TestWrapperErrorStillSwallowsNonWhitelistedSub2000Codes(t *testing.T) {
	// Guards the other half of finding 1's contract: the fix must not turn
	// the sub-2000 filter into a no-op. A code NOT on the whitelist (e.g.
	// 202 "Order Cancelled" — unrelated to market-data/contract validation)
	// must keep being dropped as informational, exactly as before —
	// regardless of strict registration.
	for _, strict := range []bool{false, true} {
		name := "non_strict"
		if strict {
			name = "strict"
		}
		t.Run(name, func(t *testing.T) {
			w := newIbWrapper()
			reqID := int64(2202)
			if strict {
				reqID = 2203
			}
			pq := w.registerQuote(reqID, "P")
			var errCh chan error
			if strict {
				errCh = w.registerStrictError(reqID)
			} else {
				errCh = w.registerError(reqID)
			}
			defer w.unregister(reqID)

			w.Error(reqID, 0, 202, "Order Cancelled - synthetic", "")

			select {
			case err := <-errCh:
				t.Fatalf("non-whitelisted code 202 reached errCh: %v", err)
			default:
			}
			select {
			case <-pq.done:
				t.Fatal("non-whitelisted code 202 closed pq.done")
			default:
			}
		})
	}
}

func TestWrapperErrorExcludes354FromStrictSetEvenWhenStrict(t *testing.T) {
	// Documented exception: option-quote (the only strict caller) must NOT
	// strict-route 354 ("Requested market data is not subscribed"). IB can
	// fire 354 before delayed/frozen ticks arrive on a contract without a
	// live-data subscription, and those ticks can still complete a usable
	// degraded quote. Strict-routing 354 would kill the quote before it had
	// a chance to degrade — this is the reviewer's finding-1 exception,
	// verified directly against the strict path (option-quote's own
	// registration), not just the non-strict callers above.
	w := newIbWrapper()
	reqID := int64(4354)
	pq := w.registerQuote(reqID, "P")
	errCh := w.registerStrictError(reqID) // option-quote's own registration
	defer w.unregister(reqID)

	w.Error(reqID, 0, 354, "Requested market data is not subscribed", "")

	select {
	case err := <-errCh:
		t.Fatalf("354 reached errCh on a strict (option-quote) reqID: %v — would kill the quote before delayed ticks land", err)
	default:
	}
	select {
	case <-pq.done:
		t.Fatal("354 closed pq.done on a strict (option-quote) reqID — delayed-tick degrade path can no longer run")
	default:
	}
}

func TestWrapperOptionQuotePath354DegradesButNotRoutedWhile200IsRouted(t *testing.T) {
	// End-to-end shape of GetOptionQuoteDetails' own error registration
	// (registerStrictError): 354 must degrade (not reach errCh, not close
	// done — the delayed-tick path gets a chance to run), while 200 (a real
	// contract-validation failure) must still fast-fail via errCh, exactly
	// as option-quote's whole reason for registering strict requires.
	t.Run("354_degrades", func(t *testing.T) {
		w := newIbWrapper()
		reqID := int64(7354)
		pq := w.registerQuote(reqID, "P")
		errCh := w.registerStrictError(reqID)
		defer w.unregister(reqID)

		w.Error(reqID, 0, 354, "Requested market data is not subscribed", "")

		select {
		case err := <-errCh:
			t.Fatalf("354 routed to errCh on option-quote's strict reqID: %v", err)
		default:
		}
		select {
		case <-pq.done:
			t.Fatal("354 closed pq.done on option-quote's strict reqID")
		default:
		}
	})

	t.Run("200_routed", func(t *testing.T) {
		w := newIbWrapper()
		reqID := int64(7200)
		pq := w.registerQuote(reqID, "P")
		errCh := w.registerStrictError(reqID)
		defer w.unregister(reqID)

		w.Error(reqID, 0, 200, "No security definition has been found for the request", "")

		select {
		case err := <-errCh:
			if !strings.Contains(err.Error(), "200") {
				t.Fatalf("errCh error = %q, want it to mention code 200", err)
			}
		default:
			t.Fatal("200 did not reach errCh on option-quote's strict reqID")
		}
		select {
		case <-pq.done:
		default:
			t.Fatal("200 did not close pq.done on option-quote's strict reqID")
		}
	})
}

// --- #193 finding 2: early-completion heuristic for streaming quotes ------

func TestWrapperQuoteClosesDoneAsSoonAsKeyFieldsArrive(t *testing.T) {
	// ReqMktData(snapshot=false) is a streaming request — TickSnapshotEnd
	// never fires for it (that callback only exists for snapshot requests),
	// so before this fix pq.done only closed via Error() or the caller's own
	// timeout: every GetOptionQuoteDetails call burned the full 5s
	// collection window even when everything it needed arrived in
	// milliseconds (#193 finding 2).
	w := newIbWrapper()
	reqID := int64(3001)
	pq := w.registerQuote(reqID, "P")
	defer w.unregister(reqID)

	w.TickPrice(reqID, ibapi.BID, 1.20, ibapi.TickAttrib{})
	w.TickPrice(reqID, ibapi.ASK, 1.25, ibapi.TickAttrib{})
	select {
	case <-pq.done:
		t.Fatal("done closed before last/mark, IV, and delta arrived")
	default:
	}

	w.TickPrice(reqID, ibapi.LAST, 1.23, ibapi.TickAttrib{})
	select {
	case <-pq.done:
		t.Fatal("done closed before IV and delta arrived")
	default:
	}

	w.TickOptionComputation(reqID, ibapi.MODEL_OPTION, 0, 0.31, -0.24, 1.23, 0, 0.01, 0.12, -0.04, 290)

	select {
	case <-pq.done:
	default:
		t.Fatal("done did not close once bid+ask+last+IV+delta were all present")
	}
}

func TestWrapperQuoteDoesNotCloseDoneWhenFieldsStayIncomplete(t *testing.T) {
	// Safety half of the heuristic: if the key fields never fully arrive,
	// `done` must stay open so the caller falls through to its existing
	// timeout — the heuristic may only ever shorten the wait, never skip
	// legitimately incomplete data.
	w := newIbWrapper()
	reqID := int64(3002)
	pq := w.registerQuote(reqID, "P")
	defer w.unregister(reqID)

	w.TickPrice(reqID, ibapi.BID, 1.20, ibapi.TickAttrib{})
	w.TickPrice(reqID, ibapi.ASK, 1.25, ibapi.TickAttrib{})
	w.TickPrice(reqID, ibapi.LAST, 1.23, ibapi.TickAttrib{})
	// IV/delta never arrive.

	select {
	case <-pq.done:
		t.Fatal("done closed without IV/delta ever arriving")
	default:
	}
}

// --- #193 finding 3: OI/volume must not leak across call/put sides --------

func TestWrapperTickSizeRejectsOppositeSideOptionTicks(t *testing.T) {
	w := newIbWrapper()
	reqID := int64(4001)
	pq := w.registerQuote(reqID, "P") // this request is for the PUT contract
	defer w.unregister(reqID)

	// IB can (and does) deliver the CALL side's aggregate volume/OI to the
	// same reqID; pre-fix, last-writer-wins meant these silently clobbered
	// the put's real volume/OI (#193 finding 3).
	w.TickSize(reqID, ibapi.OPTION_CALL_VOLUME, ibapi.StringToDecimal("9999"))
	w.TickSize(reqID, ibapi.OPTION_CALL_OPEN_INTEREST, ibapi.StringToDecimal("8888"))

	snap := pq.snapshot()
	if snap.volume != 0 || snap.openInterest != 0 {
		t.Fatalf("opposite-side (CALL) ticks leaked into PUT request: %+v", snap)
	}

	w.TickSize(reqID, ibapi.OPTION_PUT_VOLUME, ibapi.StringToDecimal("1234"))
	w.TickSize(reqID, ibapi.OPTION_PUT_OPEN_INTEREST, ibapi.StringToDecimal("5678"))

	snap = pq.snapshot()
	if snap.volume != 1234 || snap.openInterest != 5678 {
		t.Fatalf("matching-side (PUT) ticks not captured: %+v", snap)
	}
}

func TestWrapperTickSizeGenericTick86AlwaysAcceptedRegardlessOfSide(t *testing.T) {
	// Tick type 86 is already scoped to the specific contract requested
	// (not a call/put aggregate), so it must remain side-agnostic.
	w := newIbWrapper()
	reqID := int64(4002)
	pq := w.registerQuote(reqID, "C")
	defer w.unregister(reqID)

	w.TickSize(reqID, 86, ibapi.StringToDecimal("42"))

	snap := pq.snapshot()
	if snap.openInterest != 42 {
		t.Fatalf("generic tick 86 openInterest = %v, want 42", snap.openInterest)
	}
}

// --- #193 finding 4: mark and IV source priority ---------------------------

func TestWrapperMarkPrefersGenuineMarkPriceTickOverModelComputation(t *testing.T) {
	w := newIbWrapper()
	reqID := int64(5001)
	pq := w.registerQuote(reqID, "P")
	defer w.unregister(reqID)

	// MODEL_OPTION arrives first — used as a fallback mark since no genuine
	// MARK_PRICE tick has arrived yet.
	w.TickOptionComputation(reqID, ibapi.MODEL_OPTION, 0, 0.31, -0.24, 1.10, 0, 0, 0, 0, 0)
	if got := pq.snapshot().mark; got != 1.10 {
		t.Fatalf("mark after MODEL_OPTION only = %v, want 1.10 (fallback)", got)
	}

	// A genuine MARK_PRICE tick (generic tick 221) arrives — it must win
	// over the model-computed price.
	w.TickPrice(reqID, ibapi.MARK_PRICE, 1.24, ibapi.TickAttrib{})
	if got := pq.snapshot().mark; got != 1.24 {
		t.Fatalf("mark after MARK_PRICE tick = %v, want 1.24 (genuine tick)", got)
	}

	// A later MODEL_OPTION recompute must NOT override the genuine tick.
	w.TickOptionComputation(reqID, ibapi.MODEL_OPTION, 0, 0.32, -0.25, 1.11, 0, 0, 0, 0, 0)
	if got := pq.snapshot().mark; got != 1.24 {
		t.Fatalf("mark after later MODEL_OPTION = %v, want 1.24 (genuine tick must not be overwritten)", got)
	}
}

func TestWrapperImpliedVolatilityPriorityOrder(t *testing.T) {
	w := newIbWrapper()
	reqID := int64(5002)
	pq := w.registerQuote(reqID, "P")
	defer w.unregister(reqID)

	// Lowest tier: bid/ask computation midpoint.
	w.TickOptionComputation(reqID, ibapi.BID_OPTION_COMPUTATION, 0, 0.20, 0, 0, 0, 0, 0, 0, 0)
	w.TickOptionComputation(reqID, ibapi.ASK_OPTION_COMPUTATION, 0, 0.30, 0, 0, 0, 0, 0, 0, 0)
	if got := pq.snapshot().impliedVolatility; got != 0.25 {
		t.Fatalf("IV with only bid/ask computation = %v, want 0.25 (midpoint)", got)
	}

	// Middle tier: LAST_OPTION_COMPUTATION overrides the bid/ask midpoint.
	w.TickOptionComputation(reqID, ibapi.LAST_OPTION_COMPUTATION, 0, 0.40, 0, 0, 0, 0, 0, 0, 0)
	if got := pq.snapshot().impliedVolatility; got != 0.40 {
		t.Fatalf("IV after LAST_OPTION_COMPUTATION = %v, want 0.40", got)
	}

	// Highest tier: MODEL_OPTION overrides LAST.
	w.TickOptionComputation(reqID, ibapi.MODEL_OPTION, 0, 0.50, 0, 0, 0, 0, 0, 0, 0)
	if got := pq.snapshot().impliedVolatility; got != 0.50 {
		t.Fatalf("IV after MODEL_OPTION = %v, want 0.50", got)
	}

	// A later, lower-tier bid/ask update must not override MODEL.
	w.TickOptionComputation(reqID, ibapi.BID_OPTION_COMPUTATION, 0, 0.99, 0, 0, 0, 0, 0, 0, 0)
	if got := pq.snapshot().impliedVolatility; got != 0.50 {
		t.Fatalf("IV after later low-tier update = %v, want 0.50 (MODEL must stay authoritative)", got)
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

// TestWrapperMarketDepthPartialRowsSurviveNonStrictWhitelistedError guards
// GetMarketDepth's documented contract (client.go: "returns partial rows if
// IB delivered some depth before the collection timeout"). GetMarketDepth
// registers its error channel via plain registerError — depth reqIDs never
// opt into strict routing (only GetOptionQuoteDetails does). Before the
// per-reqID scoping fix, a routed 354 ("Requested market data is not
// subscribed" — e.g. no live-depth permission, a routine condition) would
// have hit errCh and made GetMarketDepth return the error instead of the
// partial snapshot it already collected (client.go's `case err := <-errCh:`
// branch discards pd.snapshot() entirely). There's no fake/mock EClient
// harness in this package for a full Client-level GetMarketDepth test
// (ibClient is a concrete *ibapi.EClient — see client.go), so this proves
// the same contract at the wrapper level: a partial depth snapshot must
// still be intact and errCh must stay silent after a non-strict 354.
func TestWrapperMarketDepthPartialRowsSurviveNonStrictWhitelistedError(t *testing.T) {
	w := newIbWrapper()
	reqID := int64(3000)
	pd := w.registerDepth(reqID, 2) // GetMarketDepth's real registration
	errCh := w.registerError(reqID) // non-strict — matches GetMarketDepth
	defer w.unregister(reqID)

	// Partial snapshot: one bid level arrived before IB reported the error.
	w.UpdateMktDepth(reqID, 0, 0, 1, 100.00, ibapi.StringToDecimal("10"))

	w.Error(reqID, 0, 354, "Requested market data is not subscribed", "")

	select {
	case err := <-errCh:
		t.Fatalf("354 on a non-strict depth reqID reached errCh: %v — GetMarketDepth would discard its partial rows", err)
	default:
	}
	rows := pd.snapshot()
	if len(rows) != 1 {
		t.Fatalf("partial depth rows lost after non-strict 354: got %d rows, want 1: %#v", len(rows), rows)
	}
	if rows[0].Side != "bid" || rows[0].Price != 100.00 || rows[0].Size != 10 {
		t.Fatalf("surviving depth row mismatch: %#v", rows[0])
	}
}

func TestWrapperRoutesClientIDInUseErrorToConnectErrors(t *testing.T) {
	w := newIbWrapper()

	w.Error(-1, 0, 326, "client id already in use", "")

	select {
	case err := <-w.connectErrors:
		if !strings.Contains(err.Error(), "326") || !strings.Contains(err.Error(), "client id already in use") {
			t.Fatalf("connect error = %q, want code and message", err)
		}
	default:
		t.Fatal("expected client-id-in-use error to reach connectErrors")
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
