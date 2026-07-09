package ibkr

import (
	"errors"
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
