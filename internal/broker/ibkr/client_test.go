package ibkr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/pkg/model"
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
