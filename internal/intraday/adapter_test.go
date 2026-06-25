package intraday

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

type fakeBroker struct {
	source string
	quote  *model.StockQuote
	bars   []model.OHLCV
}

func (b *fakeBroker) Connect(context.Context) error { return nil }

func (b *fakeBroker) Disconnect() error { return nil }

func (b *fakeBroker) IsConnected() bool { return true }

func (b *fakeBroker) SourceName() string { return b.source }

func (b *fakeBroker) GetQuote(context.Context, string) (*model.StockQuote, error) {
	return b.quote, nil
}

func (b *fakeBroker) GetHistoricalBars(context.Context, string, string, string, string) ([]model.OHLCV, error) {
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
