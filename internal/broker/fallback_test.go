package broker

import (
	"context"
	"errors"
	"testing"

	"github.com/IS908/optix/pkg/model"
)

// fakeBroker is a minimal broker.Broker for testing FallbackBroker's lifecycle.
// connectErr seeds the error returned from Connect; connectCalls / disconnectCalls
// let tests assert behavioral invariants ("fallback was/was-not attempted").
type fakeBroker struct {
	name            string
	connectErr      error
	connectCalls    int
	disconnectCalls int
	connected       bool
}

func (f *fakeBroker) Connect(context.Context) error {
	f.connectCalls++
	if f.connectErr != nil {
		return f.connectErr
	}
	f.connected = true
	return nil
}

func (f *fakeBroker) Disconnect() error {
	f.disconnectCalls++
	f.connected = false
	return nil
}

func (f *fakeBroker) IsConnected() bool { return f.connected }

func (f *fakeBroker) GetQuote(context.Context, string) (*model.StockQuote, error) {
	return nil, nil
}

func (f *fakeBroker) GetHistoricalBars(context.Context, string, string, string, string) ([]model.OHLCV, error) {
	return nil, nil
}

func (f *fakeBroker) GetOptionChain(context.Context, string, string) (*model.OptionChain, error) {
	return nil, nil
}

// TestFallbackBroker_HonorsCtxCancelBeforeFallback pins the contract for #41:
// when the context is cancelled and the primary fails (because ctx is dead),
// FallbackBroker.Connect must propagate the cancellation rather than silently
// degrading to the fallback broker.
//
// Pre-fix: any primary error triggered fallback. yfinance.Connect is a no-op,
// so a cancelled ctx ended up "successfully" connected on yfinance — the
// user's Ctrl-C was silently re-interpreted as "use delayed data."
func TestFallbackBroker_HonorsCtxCancelBeforeFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // dead on arrival

	primary := &fakeBroker{name: "primary", connectErr: context.Canceled}
	fb := &fakeBroker{name: "fallback"} // never errors

	wrapper := NewFallbackBroker(primary, fb)
	err := wrapper.Connect(ctx)
	if err == nil {
		t.Fatal("expected Connect to fail (ctx cancelled); got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected ctx.Canceled, got %v", err)
	}
	if wrapper.UsingFallback() {
		t.Errorf("UsingFallback()=true; fallback must NOT have been used when ctx was cancelled")
	}
	if fb.connectCalls != 0 {
		t.Errorf("fallback.Connect was called %d times; should be 0 (ctx cancelled)", fb.connectCalls)
	}
}

// TestFallbackBroker_PrimarySuccessNoFallback locks the happy path — primary
// connects, fallback is never touched.
func TestFallbackBroker_PrimarySuccessNoFallback(t *testing.T) {
	primary := &fakeBroker{name: "primary"} // Connect succeeds
	fb := &fakeBroker{name: "fallback"}

	wrapper := NewFallbackBroker(primary, fb)
	if err := wrapper.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if wrapper.UsingFallback() {
		t.Errorf("UsingFallback()=true; primary should be active")
	}
	if fb.connectCalls != 0 {
		t.Errorf("fallback.Connect was called %d times; should be 0 (primary succeeded)", fb.connectCalls)
	}
	if wrapper.SourceName() != "IBKR" {
		t.Errorf("SourceName()=%q, want IBKR", wrapper.SourceName())
	}
}

// TestFallbackBroker_PrimaryFailsFallbackSucceeds locks the documented
// fallback path: primary fails for a non-ctx reason, fallback is engaged,
// UsingFallback() reflects reality.
func TestFallbackBroker_PrimaryFailsFallbackSucceeds(t *testing.T) {
	primary := &fakeBroker{name: "primary", connectErr: errors.New("IBKR unreachable")}
	fb := &fakeBroker{name: "fallback"}

	wrapper := NewFallbackBroker(primary, fb)
	if err := wrapper.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !wrapper.UsingFallback() {
		t.Errorf("UsingFallback()=false; fallback should be active after primary failed")
	}
	if fb.connectCalls != 1 {
		t.Errorf("fallback.Connect was called %d times; want 1", fb.connectCalls)
	}
	if wrapper.SourceName() != "Yahoo Finance" {
		t.Errorf("SourceName()=%q, want Yahoo Finance", wrapper.SourceName())
	}
	// Primary should have been explicitly disconnected even though its Connect failed —
	// the comment in fallback.go cites this as defense against partial TCP state.
	if primary.disconnectCalls < 1 {
		t.Errorf("primary.Disconnect was called %d times after Connect failure; want ≥1", primary.disconnectCalls)
	}
}

func TestFallbackBroker_MarketDepthDelegatesToActiveBroker(t *testing.T) {
	primary := &fakeDepthBroker{
		fakeBroker: fakeBroker{name: "primary"},
		depth: &model.MarketDepth{
			Symbol: "SPY",
			Levels: []model.MarketDepthLevel{
				{Side: "bid", Position: 0, Price: 499.9, Size: 1000},
				{Side: "ask", Position: 0, Price: 500.1, Size: 900},
			},
		},
	}
	fb := &fakeBroker{name: "fallback"}

	wrapper := NewFallbackBroker(primary, fb)
	if err := wrapper.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	depth, err := wrapper.GetMarketDepth(context.Background(), "SPY", 5)
	if err != nil {
		t.Fatalf("GetMarketDepth: %v", err)
	}
	if depth.Symbol != "SPY" || len(depth.Levels) != 2 {
		t.Fatalf("depth = %#v, want SPY two-level book", depth)
	}
	if primary.depthCalls != 1 {
		t.Fatalf("primary.depthCalls = %d, want 1", primary.depthCalls)
	}
}

// TestFallbackBroker_OptionQuoteDetailsUnsupportedUsesMarketDataError pins
// #193 finding 6(b): GetOptionQuoteDetails is single-contract *market* data
// (bid/ask/mark/IV/Greeks), not account data, so the "unsupported" error it
// returns when the active broker lacks DetailedOptionQuoter must say so —
// not the account-data wording used by GetPositions/GetExecutions/
// GetOptionQuote.
func TestFallbackBroker_OptionQuoteDetailsUnsupportedUsesMarketDataError(t *testing.T) {
	primary := &fakeBroker{name: "primary"}
	fb := &fakeBroker{name: "fallback"}

	wrapper := NewFallbackBroker(primary, fb)
	if err := wrapper.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_, err := wrapper.GetOptionQuoteDetails(context.Background(), "AAPL", "20260717", "P", 290)
	if !errors.Is(err, ErrMarketDataNotSupported) {
		t.Fatalf("GetOptionQuoteDetails error = %v, want ErrMarketDataNotSupported", err)
	}
	if errors.Is(err, ErrAccountNotSupported) {
		t.Fatalf("GetOptionQuoteDetails error = %v, must NOT be ErrAccountNotSupported (market data, not account data)", err)
	}
}

func TestFallbackBroker_MarketDepthUnsupportedWhenActiveBrokerCannotProvideIt(t *testing.T) {
	primary := &fakeBroker{name: "primary"}
	fb := &fakeBroker{name: "fallback"}

	wrapper := NewFallbackBroker(primary, fb)
	if err := wrapper.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := wrapper.GetMarketDepth(context.Background(), "SPY", 5); !errors.Is(err, ErrMarketDepthNotSupported) {
		t.Fatalf("GetMarketDepth error = %v, want ErrMarketDepthNotSupported", err)
	}
}

type fakeDepthBroker struct {
	fakeBroker
	depth      *model.MarketDepth
	depthCalls int
}

func (f *fakeDepthBroker) GetMarketDepth(context.Context, string, int) (*model.MarketDepth, error) {
	f.depthCalls++
	return f.depth, nil
}
