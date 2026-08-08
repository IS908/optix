package ibkr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/pkg/model"
	"github.com/scmhub/ibapi"
)

func TestPickRequestedExpiryReturnsErrExpiryNotAvailable(t *testing.T) {
	chain := &model.OptionChain{
		Underlying: "GOOGL",
		Expirations: []model.OptionChainExpiry{
			{Expiration: "20260520"},
			{Expiration: "20260522"},
			{Expiration: "20260530"},
		},
	}
	_, err := pickRequestedExpiry(chain, "20260523")
	var miss *broker.ErrExpiryNotAvailable
	if !errors.As(err, &miss) {
		t.Fatalf("got err %v, want *ErrExpiryNotAvailable", err)
	}
	if miss.Underlying != "GOOGL" || miss.Requested != "20260523" {
		t.Errorf("unexpected fields: %+v", miss)
	}
	if len(miss.Available) != 3 {
		t.Errorf("Available len=%d, want 3", len(miss.Available))
	}
}

func TestPickRequestedExpiryFindsMatch(t *testing.T) {
	chain := &model.OptionChain{
		Expirations: []model.OptionChainExpiry{
			{Expiration: "20260520"},
			{Expiration: "20260522"},
		},
	}
	exp, err := pickRequestedExpiry(chain, "20260522")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if exp.Expiration != "20260522" {
		t.Errorf("got expiry %s, want 20260522", exp.Expiration)
	}
}

func TestPickRequestedExpiryEmptyReturnsNearest(t *testing.T) {
	chain := &model.OptionChain{
		Expirations: []model.OptionChainExpiry{
			{Expiration: "20260520"},
			{Expiration: "20260522"},
		},
	}
	exp, err := pickRequestedExpiry(chain, "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if exp.Expiration != "20260520" {
		t.Errorf("got expiry %s, want 20260520 (first)", exp.Expiration)
	}
}

func TestSpotForOIWindowUsesQuoteAsAuthoritative(t *testing.T) {
	chain := &model.OptionChain{}

	spot, authoritative := spotForOIWindow(chain, &model.StockQuote{Last: 123.45})

	if spot != 123.45 {
		t.Fatalf("spot = %v, want 123.45", spot)
	}
	if !authoritative {
		t.Fatal("quote-derived spot should be authoritative")
	}
}

func TestSpotForOIWindowMedianFallbackIsNotAuthoritative(t *testing.T) {
	chain := &model.OptionChain{
		Expirations: []model.OptionChainExpiry{
			{
				Calls: []model.OptionQuote{
					{Strike: 90},
					{Strike: 100},
					{Strike: 110},
				},
			},
		},
	}

	spot, authoritative := spotForOIWindow(chain, nil)

	if spot != 100 {
		t.Fatalf("spot = %v, want median strike 100", spot)
	}
	if authoritative {
		t.Fatal("median-strike fallback must not be treated as authoritative spot")
	}
	if chain.UnderlyingPrice != 0 {
		t.Fatalf("UnderlyingPrice = %v, want unset", chain.UnderlyingPrice)
	}
}

func TestQuoteChangeFromLastAndClose(t *testing.T) {
	change, pct := quoteChange(105, 100)
	if change != 5 {
		t.Fatalf("change = %v, want 5", change)
	}
	if pct != 5 {
		t.Fatalf("pct = %v, want 5", pct)
	}
}

func TestQuoteChangeRequiresLastAndClose(t *testing.T) {
	cases := []struct {
		name  string
		last  float64
		close float64
	}{
		{name: "missing last", last: 0, close: 100},
		{name: "missing close", last: 105, close: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			change, pct := quoteChange(tc.last, tc.close)
			if change != 0 || pct != 0 {
				t.Fatalf("change=%v pct=%v, want 0/0", change, pct)
			}
		})
	}
}

func TestQuoteMarkRequiresTwoSidedMidpoint(t *testing.T) {
	if got := quoteMark(0, 0, 100, 0, 98); got != 98 {
		t.Fatalf("quoteMark single-sided bid = %v, want close 98", got)
	}
	if got := quoteMark(0, 0, 0, 101, 98); got != 98 {
		t.Fatalf("quoteMark single-sided ask = %v, want close 98", got)
	}
	if got := quoteMark(0, 0, 99, 101, 98); got != 100 {
		t.Fatalf("quoteMark two-sided midpoint = %v, want 100", got)
	}
	if got := quoteMark(102, 101, 99, 101, 98); got != 102 {
		t.Fatalf("quoteMark mark priority = %v, want 102", got)
	}
}

func TestClientIDCandidatesUsesProcessScopedFallbacksForNonMaster(t *testing.T) {
	got := clientIDCandidates(4, 12345)
	want := []int64{4, 469100008, 469100009}
	if len(got) != len(want) {
		t.Fatalf("clientIDCandidates len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("clientIDCandidates[%d] = %d, want %d; got %#v", i, got[i], want[i], got)
		}
	}

	otherProcess := clientIDCandidates(4, 12346)
	if otherProcess[1] == got[1] {
		t.Fatalf("fallback client ID should vary by pid: %#v vs %#v", got, otherProcess)
	}
}

func TestClientIDCandidatesDoesNotFallbackMasterClientID(t *testing.T) {
	got := clientIDCandidates(0, 12345)
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("clientIDCandidates(0) = %#v, want [0]", got)
	}
}

func TestClientIDCandidatesNeverRepeatsPrimary(t *testing.T) {
	got := clientIDCandidates(1334504, 12345)
	want := []int64{1334504, 469169008, 469169009}
	if len(got) != len(want) {
		t.Fatalf("clientIDCandidates len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("clientIDCandidates[%d] = %d, want %d; got %#v", i, got[i], want[i], got)
		}
	}
}

func TestClientIDCandidatesSeparatePrimaryIDsWithSameSuffix(t *testing.T) {
	left := clientIDCandidates(7201, 24504)
	right := clientIDCandidates(8701, 24504)

	for _, leftID := range left[1:] {
		for _, rightID := range right[1:] {
			if leftID == rightID {
				t.Fatalf("fallback collision for primary IDs 7201 and 8701: %#v vs %#v", left, right)
			}
		}
	}
}

func TestRunClientIDAttemptsResetsAndRetriesClientIDInUse(t *testing.T) {
	candidates := []int64{4, 101, 102}
	resetCount := 0
	waitCount := 0
	attempted := make([]int64, 0, len(candidates))

	result, err := runClientIDAttempts(context.Background(), candidates, clientIDAttemptHooks{
		reset: func() { resetCount++ },
		connect: func(_ context.Context, clientID int64) error {
			if resetCount != len(attempted)+1 {
				t.Fatalf("connect client ID %d ran without a fresh reset", clientID)
			}
			attempted = append(attempted, clientID)
			if len(attempted) == 1 {
				return &ibConnectError{
					clientID:  clientID,
					retryable: true,
					err:       &ibAPIError{code: ibClientIDInUseCode, message: "client id already in use"},
				}
			}
			return nil
		},
		wait: func(_ context.Context, attempt int) error {
			waitCount++
			if attempt != 0 {
				t.Fatalf("wait attempt = %d, want 0", attempt)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("runClientIDAttempts: %v", err)
	}
	if !result.connected || result.clientID != 101 {
		t.Fatalf("result = %+v, want connected client ID 101", result)
	}
	if resetCount != 2 || waitCount != 1 {
		t.Fatalf("resetCount=%d waitCount=%d, want 2 and 1", resetCount, waitCount)
	}
	if len(result.errors) != 1 || len(attempted) != 2 || attempted[0] != 4 || attempted[1] != 101 {
		t.Fatalf("attempted=%v errors=%v", attempted, result.errors)
	}
}

func TestRunClientIDAttemptsDoesNotRetryMasterClientID(t *testing.T) {
	candidates := clientIDCandidates(0, 12345)
	resetCount := 0
	waitCount := 0
	result, err := runClientIDAttempts(context.Background(), candidates, clientIDAttemptHooks{
		reset: func() { resetCount++ },
		connect: func(_ context.Context, clientID int64) error {
			return &ibConnectError{clientID: clientID, retryable: true, err: errIBKRHandshakeTimeout}
		},
		wait: func(context.Context, int) error {
			waitCount++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("runClientIDAttempts: %v", err)
	}
	if result.connected || len(result.errors) != 1 || resetCount != 1 || waitCount != 0 {
		t.Fatalf("result=%+v resetCount=%d waitCount=%d, want one failed master attempt", result, resetCount, waitCount)
	}
}

func TestRunClientIDAttemptsStopsWhenRetryWaitIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result, err := runClientIDAttempts(ctx, []int64{4, 101}, clientIDAttemptHooks{
		reset: func() {},
		connect: func(_ context.Context, clientID int64) error {
			return &ibConnectError{clientID: clientID, retryable: true, err: errIBKRHandshakeTimeout}
		},
		wait: func(ctx context.Context, _ int) error {
			cancel()
			return ctx.Err()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runClientIDAttempts error = %v, want context canceled", err)
	}
	if result.connected || len(result.errors) != 1 {
		t.Fatalf("result = %+v, want one failed attempt before cancellation", result)
	}
}

func TestConnectAttemptsErrorIncludesEndpointForSingleAttempt(t *testing.T) {
	cause := errors.New("handshake failed")
	err := connectAttemptsError("127.0.0.1", 4001, []int64{4}, []error{cause})
	if !errors.Is(err, cause) {
		t.Fatalf("connectAttemptsError should wrap cause: %v", err)
	}
	for _, want := range []string{"127.0.0.1:4001", "client ID 4", "handshake failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("connectAttemptsError = %q, want substring %q", err, want)
		}
	}
}

// --- #192: handshake-retry budget --------------------------------------

// newHandshakeTestClient builds a Client wired to a fresh IbWrapper and a
// never-connected real *ibapi.EClient, for exercising awaitHandshakeLocked /
// finishConnectLocked directly against wrapper channels without a real TCP
// dial to IB Gateway/TWS. Disconnect() on an unconnected EClient is a safe
// no-op (ibapi checks IsConnected() first), so this is safe to call from the
// timeout/error paths under test.
func newHandshakeTestClient(clientID int64) *Client {
	w := newIbWrapper()
	return &Client{
		cfg:              Config{ClientID: clientID},
		wrapper:          w,
		ibClient:         ibapi.NewEClient(w),
		handshakeTimeout: ibHandshakeTimeout,
	}
}

// TestAwaitHandshakeLockedWedgedGatewayTimesOutRetryableOnce pins #192 round
// 2's hybrid retry policy — a DELIBERATE CONTRACT CHANGE from 4fbcbbb's
// TestAwaitHandshakeLockedWedgedGatewayTimesOutNonRetryable, which this test
// replaces. The adversarial review on #192 rejected 4fbcbbb's "never retry a
// timeout" design: #188/#189's wedge symptom is a handshake TIMEOUT on a
// zombie-held PRIMARY clientID (not error 326), and #189's fix was precisely
// retrying onto a fresh fallback ID — a plain "never retry timeouts" policy
// regresses that. A wedged gateway (NextValidID never arrives) must now
// produce a *ibConnectError with retryable=true AND timeout=true, so
// runClientIDAttempts retries it exactly once onto the next candidate
// (capped by maxTimeoutRetries — see the runClientIDAttempts-level tests
// below for the cap itself).
func TestAwaitHandshakeLockedWedgedGatewayTimesOutRetryableOnce(t *testing.T) {
	c := newHandshakeTestClient(4)

	start := time.Now()
	err := c.awaitHandshakeLocked(context.Background(), 4, 20*time.Millisecond)
	elapsed := time.Since(start)

	var connErr *ibConnectError
	if !errors.As(err, &connErr) {
		t.Fatalf("err = %v, want *ibConnectError", err)
	}
	if !connErr.retryable {
		t.Fatal("handshake timeout marked non-retryable; want retryable — #192 round 2 hybrid policy retries a timeout once onto a fresh clientID")
	}
	if !connErr.timeout {
		t.Fatal("handshake timeout not marked timeout=true; runClientIDAttempts needs this to cap timeout-retries separately from 326 (#192 round 2)")
	}
	if !errors.Is(err, errIBKRHandshakeTimeout) {
		t.Fatalf("err = %v, want errIBKRHandshakeTimeout", err)
	}
	if elapsed < 20*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("elapsed = %v, want ~1 handshake window (20ms), not a multiple of it", elapsed)
	}
}

// TestAwaitHandshakeLockedClientIDInUseIsRetryable pins the #189 behavior
// this fix must preserve: error 326 (clientID already in use) is classified
// retryable but NOT timeout — it resolves near-instantly against a fresh
// candidate ID and must not consume runClientIDAttempts' separate
// timeout-retry budget (#192 round 2).
func TestAwaitHandshakeLockedClientIDInUseIsRetryable(t *testing.T) {
	c := newHandshakeTestClient(4)
	c.wrapper.Error(0, 0, ibClientIDInUseCode, "client id already in use", "")

	err := c.awaitHandshakeLocked(context.Background(), 4, time.Second)

	var connErr *ibConnectError
	if !errors.As(err, &connErr) {
		t.Fatalf("err = %v, want *ibConnectError", err)
	}
	if !connErr.retryable {
		t.Fatal("clientID-in-use (326) marked non-retryable; want retryable (#189 behavior preserved)")
	}
	if connErr.timeout {
		t.Fatal("clientID-in-use (326) marked timeout=true; want false — it must not consume the timeout-retry budget (#192 round 2)")
	}
}

// TestAwaitHandshakeLockedSucceedsOnNextValidID is the control case: a
// timely NextValidID must still succeed and seed the request-ID counter.
func TestAwaitHandshakeLockedSucceedsOnNextValidID(t *testing.T) {
	c := newHandshakeTestClient(4)
	c.wrapper.NextValidID(7)

	if err := c.awaitHandshakeLocked(context.Background(), 4, time.Second); err != nil {
		t.Fatalf("awaitHandshakeLocked: %v", err)
	}
	if got := c.nextReqID(); got != 8 {
		t.Fatalf("nextReqID() = %d, want 8 (seeded from NextValidID=7)", got)
	}
}

// TestAwaitHandshakeLockedOuterCtxCancelReturnsRawCtxErr pins the existing
// #41-adjacent behavior: when the CALLER's ctx is cancelled mid-handshake
// (as opposed to this attempt's own handshakeTimeout elapsing), the raw
// ctx.Err() propagates unwrapped so FallbackBroker's ctx.Err()!=nil guardrail
// keeps working regardless of how this package wraps other failures.
func TestAwaitHandshakeLockedOuterCtxCancelReturnsRawCtxErr(t *testing.T) {
	c := newHandshakeTestClient(4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.awaitHandshakeLocked(ctx, 4, time.Minute) // handshake window far longer than the cancel
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	var connErr *ibConnectError
	if errors.As(err, &connErr) {
		t.Fatalf("err = %v, want raw ctx.Err(), not wrapped in *ibConnectError", err)
	}
}

// --- #192 round 2: hybrid retry policy (window formula) ----------------
//
// perAttemptHandshakeWindow is a pure function of ctx.Deadline() and
// time.Now(), so the walkthroughs below that only need to check the FORMULA
// (not the retry loop's timing behavior) construct ctx values with whatever
// deadline they want to represent and call it immediately — no sleeping
// required, even to exercise the #192 issue's real 15s/10s/4s/1s numbers.

func TestPerAttemptHandshakeWindowUsesDefaultWithoutDeadline(t *testing.T) {
	window, ok := perAttemptHandshakeWindow(context.Background(), 7*time.Second)
	if !ok || window != 7*time.Second {
		t.Fatalf("window=%v ok=%v, want 7s default with no ctx deadline", window, ok)
	}
}

func TestPerAttemptHandshakeWindowCapsAtDefault(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	window, ok := perAttemptHandshakeWindow(ctx, 10*time.Second)
	if !ok || window != 10*time.Second {
		t.Fatalf("window=%v ok=%v, want capped at the 10s default", window, ok)
	}
}

func TestPerAttemptHandshakeWindowExpiredCtxReturnsNotOK(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), -time.Second) // already expired
	defer cancel()

	window, ok := perAttemptHandshakeWindow(ctx, 10*time.Second)
	if ok || window != 0 {
		t.Fatalf("window=%v ok=%v, want 0/false (budget already exhausted)", window, ok)
	}
}

// TestPerAttemptHandshakeWindowFloorBlocksAttempt pins the #192 issue's
// window-floor walkthrough: 2.5s remaining ctx budget minus the 1s
// fallbackHandshakeReserve leaves 1.5s, below the 2s minHandshakeWindow
// floor, so perAttemptHandshakeWindow must refuse the attempt entirely
// (Connect()'s hook then returns the accumulated error immediately without
// dialing IB) rather than let a doomed sub-floor attempt burn the reserve.
func TestPerAttemptHandshakeWindowFloorBlocksAttempt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	window, ok := perAttemptHandshakeWindow(ctx, 10*time.Second)
	if ok {
		t.Fatalf("ok = true (window=%v), want false: 1.5s remaining after the 1s reserve is below the 2s floor", window)
	}
	if window != 0 {
		t.Fatalf("window = %v, want 0 when not attempting", window)
	}
	if ctx.Err() != nil {
		t.Fatalf("ctx.Err() = %v, want nil (checking the floor must not touch ctx)", ctx.Err())
	}
}

// TestPerAttemptHandshakeWindowPoolPathWalkthrough pins the #192 issue's
// pool-path walkthrough (ctx deadline 15s, mirroring the web UI broker
// pool's reconnectTimeout; handshakeDefault 10s) at the formula level: the
// primary attempt's window hits the 10s def cap (15s-1s reserve=14s > def),
// and — modeled as a second ctx representing "5s remaining", i.e. as if the
// primary's full 10s window had already elapsed — the retry's window
// shrinks to min(10s, 5s-1s reserve)=4s, exactly the numbers in the issue.
func TestPerAttemptHandshakeWindowPoolPathWalkthrough(t *testing.T) {
	const def = 10 * time.Second

	primaryCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	window1, ok1 := perAttemptHandshakeWindow(primaryCtx, def)
	if !ok1 {
		t.Fatal("primary window: ok = false, want true")
	}
	if window1 != def {
		t.Fatalf("primary window = %v, want exactly %v (capped at def; 15s-1s reserve=14s > def)", window1, def)
	}

	retryCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second) // "as if" the primary's 10s window already elapsed from the 15s budget
	defer cancel2()
	window2, ok2 := perAttemptHandshakeWindow(retryCtx, def)
	if !ok2 {
		t.Fatal("retry window: ok = false, want true")
	}
	if window2 < 3900*time.Millisecond || window2 > 4*time.Second {
		t.Fatalf("retry window = %v, want ~4s (min(10s, 5s-1s reserve))", window2)
	}
}

// --- #192 round 2: hybrid retry policy (retry-loop mechanics) -----------

// TestRunClientIDAttemptsNoDeadlineTimeoutRetriesOnceThenSucceeds mirrors
// the #192 issue's no-deadline walkthrough — `optix positions` runs with a
// fixed ClientID 4 under context.Background() and has no yfinance
// equivalent (AccountReader isn't implemented by the yfinance broker), so a
// hard-fail on the first zombie-held-primary timeout would have no
// fallback. The hybrid policy retries the timeout once onto the next
// candidate; connecting on that SECOND attempt is exactly the #189 escape
// hatch the #192 adversarial review required this fix to restore.
func TestRunClientIDAttemptsNoDeadlineTimeoutRetriesOnceThenSucceeds(t *testing.T) {
	candidates := []int64{4, 101, 102}
	connectCalls := 0
	waitCalls := 0

	result, err := runClientIDAttempts(context.Background(), candidates, clientIDAttemptHooks{
		reset: func() {},
		connect: func(_ context.Context, clientID int64) error {
			connectCalls++
			if connectCalls == 1 {
				return &ibConnectError{clientID: clientID, retryable: true, timeout: true, err: errIBKRHandshakeTimeout}
			}
			return nil // fresh clientID succeeds immediately (#189 live evidence)
		},
		wait: func(context.Context, int) error {
			waitCalls++
			return nil
		},
	})

	if err != nil {
		t.Fatalf("runClientIDAttempts: %v", err)
	}
	if !result.connected || result.clientID != 101 {
		t.Fatalf("result = %+v, want connected on fallback clientID 101", result)
	}
	if connectCalls != 2 {
		t.Fatalf("connectCalls = %d, want 2 (one timeout-retry then success)", connectCalls)
	}
	if waitCalls != 1 {
		t.Fatalf("waitCalls = %d, want 1", waitCalls)
	}
}

// TestRunClientIDAttemptsNoDeadlineSecondTimeoutAbortsWithoutThirdAttempt
// pins #192 round 2's hybrid retry cap at the retry-loop level. This is a
// DELIBERATE CONTRACT CHANGE from 4fbcbbb's
// TestRunClientIDAttemptsWedgedHandshakeTimeoutDoesNotRetry, which this test
// replaces: 4fbcbbb asserted a wedged gateway stops after exactly ONE
// attempt (it never retried a timeout at all). The #192 adversarial review
// rejected that design — #188/#189's wedge symptom IS a handshake timeout
// on a zombie-held PRIMARY clientID, and #189's whole fix was retrying onto
// a fresh fallback ID, so never retrying timeouts regressed #189's core
// scenario. The hybrid policy now retries a handshake timeout exactly ONCE
// onto the next candidate; a SECOND timeout means even a fresh clientID
// wedged (the gateway itself is down/unresponsive), so THAT is where
// retries stop — after 2 attempts, not 1 and not the full 3-candidate
// chain.
func TestRunClientIDAttemptsNoDeadlineSecondTimeoutAbortsWithoutThirdAttempt(t *testing.T) {
	candidates := []int64{4, 101, 102}
	connectCalls := 0
	waitCalls := 0
	const window = 5 * time.Millisecond

	result, err := runClientIDAttempts(context.Background(), candidates, clientIDAttemptHooks{
		reset: func() {},
		connect: func(_ context.Context, clientID int64) error {
			connectCalls++
			time.Sleep(window) // simulate the handshake window actually elapsing
			return &ibConnectError{clientID: clientID, retryable: true, timeout: true, err: errIBKRHandshakeTimeout}
		},
		wait: func(context.Context, int) error {
			waitCalls++
			return nil
		},
	})

	if err != nil {
		t.Fatalf("runClientIDAttempts: %v", err)
	}
	if result.connected {
		t.Fatal("result.connected = true, want false (every candidate wedges)")
	}
	if connectCalls != 2 {
		t.Fatalf("connect called %d times, want 2 (one timeout-retry, then a SECOND timeout aborts — never a 3rd attempt on the last candidate)", connectCalls)
	}
	if waitCalls != 1 {
		t.Fatalf("wait called %d times, want 1 (only the first timeout retries; the second aborts before waiting)", waitCalls)
	}
	if len(result.errors) != 2 {
		t.Fatalf("errors = %v, want 2", result.errors)
	}
}

// TestRunClientIDAttemptsClientIDInUseDoesNotConsumeTimeoutRetryBudget pins
// the independence of the two retry counters (#192 round 2): error 326
// (clientID-in-use) retries via the ordinary candidates-list bound and must
// NOT touch runClientIDAttempts' separate timeout-retry cap. A fast 326 on
// the primary, followed by a genuine handshake timeout on the first
// fallback (the one timeout-retry the hybrid policy allows), followed by
// success on the second fallback, proves the 326 didn't burn the
// timeout-retry budget — if it had, the timeout on attempt 2 would have hit
// the cap and aborted instead of reaching attempt 3. The candidates list
// still bounds the total at 3 attempts either way.
func TestRunClientIDAttemptsClientIDInUseDoesNotConsumeTimeoutRetryBudget(t *testing.T) {
	candidates := []int64{4, 101, 102}
	var attempted []int64

	result, err := runClientIDAttempts(context.Background(), candidates, clientIDAttemptHooks{
		reset: func() {},
		connect: func(_ context.Context, clientID int64) error {
			attempted = append(attempted, clientID)
			switch len(attempted) {
			case 1:
				// Fast 326 rejection on the primary — retryable, not a timeout.
				return &ibConnectError{clientID: clientID, retryable: true, timeout: false, err: &ibAPIError{code: ibClientIDInUseCode}}
			case 2:
				// A genuine handshake timeout on the first fallback ID — the
				// one timeout-retry the hybrid policy allows.
				return &ibConnectError{clientID: clientID, retryable: true, timeout: true, err: errIBKRHandshakeTimeout}
			default:
				return nil // success on the second fallback ID
			}
		},
		wait: func(context.Context, int) error { return nil },
	})

	if err != nil {
		t.Fatalf("runClientIDAttempts: %v", err)
	}
	if !result.connected || result.clientID != 102 {
		t.Fatalf("result = %+v, want connected on clientID 102 (326 must not have burned the timeout-retry budget)", result)
	}
	if len(attempted) != 3 {
		t.Fatalf("attempted = %v, want all 3 candidates tried (326 + one timeout-retry + success)", attempted)
	}
}

// scaleHandshakeBudgetForTest temporarily shrinks fallbackHandshakeReserve
// and minHandshakeWindow (both package `var`s, defaulting to the production
// 1s/2s) so a test can exercise perAttemptHandshakeWindow's real formula,
// wired exactly the way Client.Connect's `connect` hook wires it, over a
// multi-second ctx-budget walkthrough (e.g. the web UI broker pool's 15s
// reconnectTimeout) in milliseconds instead of real wall-clock seconds. The
// ratio between def, the ctx deadline, reserve, and minWindow is preserved —
// only the absolute scale shrinks, so what's exercised is the identical
// formula and retry-loop code, just faster.
func scaleHandshakeBudgetForTest(t *testing.T, reserve, minWindow time.Duration) {
	t.Helper()
	origReserve, origMinWindow := fallbackHandshakeReserve, minHandshakeWindow
	fallbackHandshakeReserve, minHandshakeWindow = reserve, minWindow
	t.Cleanup(func() {
		fallbackHandshakeReserve, minHandshakeWindow = origReserve, origMinWindow
	})
}

// TestConnectHookPoolPathWalkthroughTimeoutRetryThenAbort mirrors the #192
// issue's pool-path walkthrough end to end, wiring the REAL
// perAttemptHandshakeWindow formula exactly the way Client.Connect's
// `connect` hook wires it: a ctx deadline shaped like the web UI broker
// pool's reconnectTimeout (real: 15s; scaled here to 300ms), a wedged
// gateway (every attempt times out), handshakeDefault (real: 10s; scaled to
// 150ms), and fallbackHandshakeReserve/minHandshakeWindow scaled
// proportionally (real: 1s/2s; scaled to 30ms/50ms) via
// scaleHandshakeBudgetForTest — so the test finishes in well under a second
// instead of ~14 real seconds, while exercising the identical formula and
// retry-loop code the production path uses.
//
// Walkthrough: the primary window hits the def cap (150ms, wedge) → the
// timeout-retry window shrinks to the remaining budget minus the reserve
// (wedge again) → the SECOND timeout aborts (maxTimeoutRetries=1) → ctx
// still has budget left alive, matching the real walkthrough's "abort at
// ~14s, 1s remains, FallbackBroker proceeds to yfinance."
func TestConnectHookPoolPathWalkthroughTimeoutRetryThenAbort(t *testing.T) {
	scaleHandshakeBudgetForTest(t, 30*time.Millisecond, 50*time.Millisecond)

	const def = 150 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	var windows []time.Duration
	result, err := runClientIDAttempts(ctx, []int64{4, 101, 102}, clientIDAttemptHooks{
		reset: func() {},
		connect: func(ctx context.Context, clientID int64) error {
			window, ok := perAttemptHandshakeWindow(ctx, def)
			if !ok {
				return &ibConnectError{clientID: clientID, err: errors.New("insufficient budget")}
			}
			windows = append(windows, window)
			time.Sleep(window) // simulate the handshake window actually elapsing (wedge)
			return &ibConnectError{clientID: clientID, retryable: true, timeout: true, err: errIBKRHandshakeTimeout}
		},
		wait: func(context.Context, int) error { return nil },
	})

	if err != nil {
		t.Fatalf("runClientIDAttempts: %v", err)
	}
	if result.connected {
		t.Fatal("result.connected = true, want false (both attempts wedge)")
	}
	if len(windows) != 2 {
		t.Fatalf("attempts = %d, want 2 (one timeout-retry, then abort — not a 3rd)", len(windows))
	}
	if windows[0] != def {
		t.Fatalf("windows[0] = %v, want exactly %v (def cap: 300ms-30ms reserve=270ms > def)", windows[0], def)
	}
	if windows[1] <= 0 || windows[1] >= windows[0] {
		t.Fatalf("windows[1] = %v, want in (0, %v) — shrinking budget as real time is consumed between attempts", windows[1], windows[0])
	}
	if ctx.Err() != nil {
		t.Fatalf("ctx.Err() = %v, want nil (the reserve must keep ctx alive so FallbackBroker's #41 guard still lets yfinance run)", ctx.Err())
	}
}

// TestFinishConnectLockedDoesNotDrainDisconnectCh pins #192 finding 2/4: a
// disconnect signal sitting in the wrapper's buffered disconnectCh when
// finishConnectLocked runs is NOT stale leftover from a prior session (every
// connect attempt gets a fresh wrapper via resetAPIClientLocked) — it is a
// genuine drop that happened between NextValidID and finishConnectLocked.
// Draining it would leave connected=true on a dead socket until the next 30s
// Ping cycle; finishConnectLocked must leave it alone so watchDisconnect
// (started right after) observes it immediately.
func TestFinishConnectLockedDoesNotDrainDisconnectCh(t *testing.T) {
	c := newHandshakeTestClient(44)

	c.mu.Lock()
	// Simulate a genuine TCP drop landing between the handshake completing
	// and finishConnectLocked running — buffered 1, exactly like the real
	// ConnectionClosed callback delivers it.
	c.wrapper.ConnectionClosed()
	c.finishConnectLocked()
	c.mu.Unlock()
	t.Cleanup(func() {
		c.mu.Lock()
		if c.watchCancel != nil {
			c.watchCancel()
		}
		c.mu.Unlock()
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && c.IsConnected() {
		time.Sleep(time.Millisecond)
	}
	if c.IsConnected() {
		t.Fatal("connected still true after 1s; finishConnectLocked drained the genuine disconnect signal (#192 regression)")
	}
}

func TestOptionQuoteFromSnapshotBuildsValidationQuote(t *testing.T) {
	q := optionQuoteFromSnapshot("AAPL", "20260717", "P", 290, quoteSnapshot{
		bid:               1.20,
		ask:               1.25,
		mark:              1.23,
		last:              1.22,
		volume:            1234,
		openInterest:      5678,
		impliedVolatility: 0.31,
		greeks:            model.Greeks{Delta: -0.24, Gamma: 0.01, Theta: -0.04, Vega: 0.12},
		marketDataType:    "real_time",
	}, []string{"ibkr_error: partial data"})

	if q.Underlying != "AAPL" || q.Expiration != "20260717" || q.OptionType != model.OptionTypePut || q.Strike != 290 {
		t.Fatalf("identity mismatch: %+v", q)
	}
	if q.Mid != 1.225 || q.Mark != 1.23 || q.Last != 1.22 {
		t.Fatalf("price fields mismatch: %+v", q)
	}
	if q.Volume != 1234 || q.OpenInterest != 5678 || q.ImpliedVolatility != 0.31 {
		t.Fatalf("availability fields mismatch: %+v", q)
	}
	if q.Greeks.Delta != -0.24 || q.MarketDataType != "real_time" {
		t.Fatalf("greeks/market data mismatch: %+v", q)
	}
	if len(q.Warnings) != 1 || q.Warnings[0] != "ibkr_error: partial data" {
		t.Fatalf("warnings = %#v", q.Warnings)
	}
}

// TestFirstIBKRErrorWarningExtractsRealIBErrorText pins #193 finding 5: when
// GetOptionQuoteDetails failed to collect price data because IB rejected the
// request (e.g. errCode 200 "no security definition"), that detail lands in
// Warnings as "ibkr_error: ...". GetOptionQuote must surface it verbatim
// instead of collapsing every failure into the generic "no price data".
func TestFirstIBKRErrorWarningExtractsRealIBErrorText(t *testing.T) {
	q := &model.OptionQuote{Warnings: []string{
		"bid_unavailable",
		"ibkr_error: IB error 200: No security definition has been found for the request",
		"no_price_data",
	}}
	got := firstIBKRErrorWarning(q)
	want := "IB error 200: No security definition has been found for the request"
	if got != want {
		t.Fatalf("firstIBKRErrorWarning = %q, want %q", got, want)
	}
}

func TestFirstIBKRErrorWarningReturnsEmptyWithoutIBKRErrorWarning(t *testing.T) {
	cases := []struct {
		name string
		q    *model.OptionQuote
	}{
		{name: "nil quote", q: nil},
		{name: "no warnings", q: &model.OptionQuote{}},
		{name: "only derived warnings", q: &model.OptionQuote{Warnings: []string{"bid_unavailable", "no_price_data"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstIBKRErrorWarning(tc.q); got != "" {
				t.Fatalf("firstIBKRErrorWarning = %q, want empty", got)
			}
		})
	}
}

func TestHistoricalQuoteFromBarsUsesPreviousCloseForChange(t *testing.T) {
	q, err := historicalQuoteFromBars("AAPL", []model.OHLCV{
		{Close: 100, Timestamp: time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)},
		{Close: 105, Timestamp: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Open: 102, High: 106, Low: 101, Volume: 123},
	})
	if err != nil {
		t.Fatalf("historicalQuoteFromBars: %v", err)
	}
	if q.Last != 105 || q.Close != 100 {
		t.Fatalf("Last/Close = %v/%v, want 105/100", q.Last, q.Close)
	}
	if q.Change != 5 || q.ChangePct != 5 {
		t.Fatalf("Change/Pct = %v/%v, want 5/5", q.Change, q.ChangePct)
	}
}

func TestHistoricalQuoteFromBarsRequiresBars(t *testing.T) {
	_, err := historicalQuoteFromBars("AAPL", nil)
	if err == nil {
		t.Fatal("expected error for no bars")
	}
}

// TestParseIBDateHandlesTrailingZoneToken is a live-E2E-discovered
// regression test (#191 verification): IB Gateway actually sends intraday
// bar dates as "20260807 09:30:00 US/Eastern" (a trailing zone token), not
// the two-field "20260807 09:30:00" the old parser assumed.
//
// The zone token is NY *wall-clock* labeling (IB formatDate=1), not a
// discardable comment: "09:30:00 US/Eastern" names the actual NYSE/NASDAQ
// session open. A prior version of this test asserted the wrong thing —
// that the two leading fields get parsed as UTC-labeled text with the zone
// suffix dropped — which is the exact regression an adversarial review
// caught on #191: parsing NY wall-clock as UTC shifts every intraday bar
// 4-5h early, so downstream sessionOpenAndVolume's `Hour() < 9` filter (see
// internal/intraday/service.go) rejects the true 09:30 ET open bar.
func TestParseIBDateHandlesTrailingZoneToken(t *testing.T) {
	got, err := parseIBDate("20260807 09:30:00 US/Eastern")
	if err != nil {
		t.Fatalf("parseIBDate returned error: %v", err)
	}
	easternLoc, locErr := time.LoadLocation("America/New_York")
	if locErr != nil {
		t.Fatalf("time.LoadLocation(America/New_York) error: %v", locErr)
	}
	want := time.Date(2026, 8, 7, 9, 30, 0, 0, easternLoc)
	if !got.Equal(want) {
		t.Fatalf("parseIBDate = %v, want %v (same instant as 09:30 ET)", got, want)
	}
	if gotHour := got.In(easternLoc).Hour(); gotHour != 9 {
		t.Fatalf("parseIBDate(...).In(NY).Hour() = %d, want 9 (the true session open, not shifted by the UTC-mislabel bug)", gotHour)
	}
}

// TestParseIBDateFallsBackToNYForUnrecognizedZoneToken covers the case
// where IB (or a test fixture) sends a zone token that isn't a real IANA
// name. Every parseIBDate caller in this codebase deals in US stock bars,
// whose session zone is always US/Eastern, so falling back to NY is the
// documented, deliberate default rather than a silent UTC mislabel.
func TestParseIBDateFallsBackToNYForUnrecognizedZoneToken(t *testing.T) {
	got, err := parseIBDate("20260807 09:30:00 Bogus/Zone")
	if err != nil {
		t.Fatalf("parseIBDate returned error: %v", err)
	}
	easternLoc, locErr := time.LoadLocation("America/New_York")
	if locErr != nil {
		t.Fatalf("time.LoadLocation(America/New_York) error: %v", locErr)
	}
	want := time.Date(2026, 8, 7, 9, 30, 0, 0, easternLoc)
	if !got.Equal(want) {
		t.Fatalf("parseIBDate with unknown zone = %v, want %v (NY fallback)", got, want)
	}
	if got.In(time.UTC).Hour() == 9 {
		t.Fatalf("parseIBDate with unknown zone landed on UTC 09:00, want NY fallback (not UTC mislabel)")
	}
}

func TestParseIBDateStillHandlesDateOnlyAndNoZoneDatetime(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Time
	}{
		{"date only", "20260807", time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)},
		{"date+time, no zone", "20260807 09:30:00", time.Date(2026, 8, 7, 9, 30, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseIBDate(tc.in)
			if err != nil {
				t.Fatalf("parseIBDate(%q) error: %v", tc.in, err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("parseIBDate(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseIBDateEmptyStringReturnsError(t *testing.T) {
	if _, err := parseIBDate(""); err == nil {
		t.Fatal("expected error for empty date string")
	}
}

// TestHistoricalDurationEmptyStartDatePreservesOneYearDefault locks the
// unchanged behavior for callers that never supply a startDate (e.g. daily
// bar fetches elsewhere in the codebase) — #191 only touches the has-a-range
// branch.
func TestHistoricalDurationEmptyStartDatePreservesOneYearDefault(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	if got := historicalDuration("", "", now); got != "1 Y" {
		t.Fatalf("historicalDuration(\"\",\"\") = %q, want \"1 Y\"", got)
	}
}

func TestHistoricalDurationBucketsByStartDateDistance(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		startDate string
		endDate   string
		want      string
	}{
		{"same day (#191 intraday lookback)", "20260625", "", "1 D"},
		{"one day back", "20260624", "", "1 D"},
		{"one week back", "20260619", "", "1 W"},
		{"three weeks back", "20260605", "", "1 M"},
		{"three months back", "20260325", "", "6 M"},
		{"explicit endDate narrows the window", "20260624", "20260625", "1 D"},
		{"malformed startDate falls back to 6 M", "not-a-date", "", "6 M"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := historicalDuration(tc.startDate, tc.endDate, now); got != tc.want {
				t.Fatalf("historicalDuration(%q,%q) = %q, want %q", tc.startDate, tc.endDate, got, tc.want)
			}
		})
	}
}
