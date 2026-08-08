package intraday

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/intelshared"
	"github.com/IS908/optix/pkg/model"
)

type fakeBroker struct {
	source string
	quote  *model.StockQuote
	bars   []model.OHLCV

	// gotStartDate records the startDate argument of the most recent
	// GetHistoricalBars call (#191 finding 1 regression coverage).
	gotStartDate string
}

func (b *fakeBroker) Connect(context.Context) error { return nil }

func (b *fakeBroker) Disconnect() error { return nil }

func (b *fakeBroker) IsConnected() bool { return true }

func (b *fakeBroker) SourceName() string { return b.source }

func (b *fakeBroker) GetQuote(context.Context, string) (*model.StockQuote, error) {
	return b.quote, nil
}

func (b *fakeBroker) GetHistoricalBars(_ context.Context, _, _, startDate, _ string) ([]model.OHLCV, error) {
	b.gotStartDate = startDate
	return b.bars, nil
}

func (b *fakeBroker) GetOptionChain(context.Context, string, string) (*model.OptionChain, error) {
	return nil, nil
}

func TestBrokerSourceAnnotatesActualQuoteSource(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	src := NewBrokerSource(&fakeBroker{
		source: "Yahoo Finance",
		quote:  &model.StockQuote{Symbol: "AAPL", Last: 110, Close: 109, Timestamp: now},
	}, "", "")

	quotes, err := src.Quotes(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	q := quotes["AAPL"]
	if q.Source != "yfinance" || q.Basis != "delayed" {
		t.Fatalf("source/basis = %s/%s, want yfinance/delayed", q.Source, q.Basis)
	}
	if q.Last != 110 || q.AsOf != now {
		t.Fatalf("quote = %+v, want last/as_of from broker", q)
	}
}

func TestBrokerSourceKeepsPreferredSourceLabel(t *testing.T) {
	src := NewBrokerSourceWithConnector(nil, "ibkr-preferred", "realtime")
	if src.SourceName() != "ibkr-preferred" {
		t.Fatalf("source = %s, want ibkr-preferred", src.SourceName())
	}
}

func TestBrokerSourceBarsDelegateToBroker(t *testing.T) {
	want := []model.OHLCV{{Timestamp: time.Date(2026, 6, 25, 13, 30, 0, 0, time.UTC), Open: 100}}
	src := NewBrokerSource(&fakeBroker{source: "IBKR", bars: want}, "", "")

	bars, err := src.Bars(context.Background(), []string{"AAPL"}, "5 mins", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars["AAPL"]) != 1 || bars["AAPL"][0].Open != 100 {
		t.Fatalf("bars = %+v, want delegated broker bars", bars)
	}
}

func TestBrokerSourceSnapshotUsesOneBrokerConnectionForQuotesAndBars(t *testing.T) {
	connects := 0
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	src := NewBrokerSourceWithConnector(func(context.Context) (broker.Broker, string, error) {
		connects++
		return &fakeBroker{
			source: "Yahoo Finance",
			quote:  &model.StockQuote{Symbol: "AAPL", Last: 110, Timestamp: now},
			bars:   []model.OHLCV{{Timestamp: now, Open: 100}},
		}, "Yahoo Finance", nil
	}, "", "")

	quotes, bars, err := src.Snapshot(context.Background(), []string{"AAPL"}, "5 mins", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if connects != 1 {
		t.Fatalf("connects = %d, want 1", connects)
	}
	if quotes["AAPL"].Source != "yfinance" || quotes["AAPL"].Basis != "delayed" {
		t.Fatalf("quote source/basis = %s/%s, want yfinance/delayed", quotes["AAPL"].Source, quotes["AAPL"].Basis)
	}
	if len(bars["AAPL"]) != 1 {
		t.Fatalf("bars = %+v, want one delegated bar", bars)
	}
}

// TestBrokerSourceBarsPassesLookbackDerivedStartDate is the #191 finding-1
// regression test: previously Bars() always called GetHistoricalBars with
// startDate="", which drove the IBKR client into its "1 Y" duration default
// (rejected for 5-min bars) and the yfinance client into its 365-day
// default (empty past yfinance's ~60-day 5m cap). The adapter must derive a
// real startDate from the requested lookback.
func TestBrokerSourceBarsPassesLookbackDerivedStartDate(t *testing.T) {
	fb := &fakeBroker{source: "IBKR", bars: []model.OHLCV{{Open: 1}}}
	src := NewBrokerSource(fb, "", "")
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC) // 11:00 ET
	src.Now = func() time.Time { return now }

	if _, err := src.Bars(context.Background(), []string{"AAPL"}, "5 mins", 8*time.Hour); err != nil {
		t.Fatal(err)
	}

	want := now.Add(-8 * time.Hour).In(intelshared.NY()).Format("20060102")
	if fb.gotStartDate != want {
		t.Fatalf("startDate = %q, want %q (lookback-derived NY calendar date)", fb.gotStartDate, want)
	}
	if fb.gotStartDate == "" {
		t.Fatalf("startDate empty — Bars() must not fall back to the broker's blind multi-month/year default")
	}
}

func TestBrokerSourceSnapshotPassesLookbackDerivedStartDate(t *testing.T) {
	fb := &fakeBroker{source: "IBKR", bars: []model.OHLCV{{Open: 1}}}
	src := NewBrokerSource(fb, "", "")
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	src.Now = func() time.Time { return now }

	if _, _, err := src.Snapshot(context.Background(), []string{"AAPL"}, "5 mins", 8*time.Hour); err != nil {
		t.Fatal(err)
	}

	want := now.Add(-8 * time.Hour).In(intelshared.NY()).Format("20060102")
	if fb.gotStartDate != want {
		t.Fatalf("startDate = %q, want %q", fb.gotStartDate, want)
	}
}

// TestBrokerSourceDoesNotScaleIBKRBarVolume is the #191 finding-4 regression
// test. The issue's audit asserted IBKR historical-bar volume needed a x100
// lots-to-shares scale-up; live verification against IB Gateway during this
// fix disproved that for the ibapi client actually vendored here (Decimal
// Bar.Volume — the modern, already-in-shares convention introduced for
// fractional-share support). Applying x100 anyway inflated real numbers by
// two orders of magnitude (see normalizeBarVolume's doc comment in
// adapter.go for the live evidence), which would have been a strictly worse
// bug than the one being fixed. So: no scaling, for either source.
func TestBrokerSourceDoesNotScaleIBKRBarVolume(t *testing.T) {
	fb := &fakeBroker{source: "IBKR", bars: []model.OHLCV{{Open: 100, Volume: 1272919}}}
	src := NewBrokerSource(fb, "", "")

	bars, err := src.Bars(context.Background(), []string{"AAPL"}, "5 mins", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got := bars["AAPL"][0].Volume; got != 1272919 {
		t.Fatalf("IBKR bar volume = %d, want unscaled 1272919 (this Decimal-based ibapi client already reports shares)", got)
	}
}

// TestBrokerSourceDoesNotScaleYfinanceBarVolume locks the other half of
// finding 4: yfinance already reports shares, so it must NOT be scaled.
func TestBrokerSourceDoesNotScaleYfinanceBarVolume(t *testing.T) {
	fb := &fakeBroker{source: "Yahoo Finance", bars: []model.OHLCV{{Open: 100, Volume: 500}}}
	src := NewBrokerSource(fb, "", "")

	bars, err := src.Bars(context.Background(), []string{"AAPL"}, "5 mins", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got := bars["AAPL"][0].Volume; got != 500 {
		t.Fatalf("yfinance bar volume = %d, want unscaled 500", got)
	}
}

// countingBroker records call counts and lets a test trigger context
// cancellation after a fixed number of broker calls, to verify
// quotesFromBroker/barsFromBroker abort promptly instead of grinding
// through the remaining symbols (#191 finding 2).
type countingBroker struct {
	quoteCalls  int
	barCalls    int
	cancel      context.CancelFunc
	cancelAfter int
}

func (b *countingBroker) Connect(context.Context) error { return nil }

func (b *countingBroker) Disconnect() error { return nil }

func (b *countingBroker) IsConnected() bool { return true }

func (b *countingBroker) SourceName() string { return "IBKR" }

func (b *countingBroker) GetQuote(_ context.Context, symbol string) (*model.StockQuote, error) {
	b.quoteCalls++
	if b.quoteCalls == b.cancelAfter {
		b.cancel()
	}
	return &model.StockQuote{Symbol: symbol, Last: 100}, nil
}

func (b *countingBroker) GetHistoricalBars(context.Context, string, string, string, string) ([]model.OHLCV, error) {
	b.barCalls++
	if b.barCalls == b.cancelAfter {
		b.cancel()
	}
	return []model.OHLCV{{Open: 100}}, nil
}

func (b *countingBroker) GetOptionChain(context.Context, string, string) (*model.OptionChain, error) {
	return nil, nil
}

func TestQuotesFromBrokerAbortsPromptlyOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &countingBroker{cancel: cancel, cancelAfter: 2}
	symbols := []string{"A", "B", "C", "D", "E"}

	quotes := quotesFromBroker(ctx, b, "ibkr", symbols)

	if b.quoteCalls != 2 {
		t.Fatalf("quoteCalls = %d, want 2 (loop must stop as soon as ctx is cancelled)", b.quoteCalls)
	}
	if len(quotes) >= len(symbols) {
		t.Fatalf("quotes = %d entries, want fewer than the full %d-symbol universe", len(quotes), len(symbols))
	}
}

func TestBarsFromBrokerAbortsPromptlyOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &countingBroker{cancel: cancel, cancelAfter: 2}
	symbols := []string{"A", "B", "C", "D", "E"}

	bars := barsFromBroker(ctx, b, "ibkr", symbols, "5 mins", "20260625")

	if b.barCalls != 2 {
		t.Fatalf("barCalls = %d, want 2 (loop must stop as soon as ctx is cancelled)", b.barCalls)
	}
	if len(bars) >= len(symbols) {
		t.Fatalf("bars = %d entries, want fewer than the full %d-symbol universe", len(bars), len(symbols))
	}
}
