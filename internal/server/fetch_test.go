package server

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/pkg/model"
)

// fakeOIBroker implements broker.Broker + broker.OIFetcher. It captures the
// (symbol, expiration) tuple passed to GetOptionChainWithOI so tests can
// assert that FetchSymbolDataOpt threads FetchOptions.Expiry through.
type fakeOIBroker struct {
	bars                   []model.OHLCV
	onGetOptionChainWithOI func(ctx context.Context, symbol, expiration string) (*model.OptionChain, error)
}

func (f *fakeOIBroker) Connect(context.Context) error { return nil }
func (f *fakeOIBroker) Disconnect() error             { return nil }
func (f *fakeOIBroker) IsConnected() bool             { return true }

func (f *fakeOIBroker) GetQuote(_ context.Context, sym string) (*model.StockQuote, error) {
	return &model.StockQuote{Symbol: sym, Last: 100, Timestamp: time.Now().UTC()}, nil
}

func (f *fakeOIBroker) GetHistoricalBars(_ context.Context, _ string, _ string, _ string, _ string) ([]model.OHLCV, error) {
	return f.bars, nil
}

func (f *fakeOIBroker) GetOptionChain(_ context.Context, sym, _ string) (*model.OptionChain, error) {
	return &model.OptionChain{Underlying: sym}, nil
}

func (f *fakeOIBroker) GetOptionChainWithOI(ctx context.Context, symbol, expiration string) (*model.OptionChain, error) {
	if f.onGetOptionChainWithOI != nil {
		return f.onGetOptionChainWithOI(ctx, symbol, expiration)
	}
	return &model.OptionChain{Underlying: symbol}, nil
}

// makeBars returns n synthetic daily bars ending today so the freshness check
// (most recent bar within 48h) passes and the cache short-circuit avoids the
// broker fetch path.
func makeBars(n int) []model.OHLCV {
	bars := make([]model.OHLCV, n)
	now := time.Now().UTC().Truncate(24 * time.Hour)
	for i := 0; i < n; i++ {
		bars[i] = model.OHLCV{
			Timestamp: now.AddDate(0, 0, -(n - 1 - i)),
			Open:      100, High: 101, Low: 99, Close: 100, Volume: 1000,
		}
	}
	return bars
}

func TestFetchSymbolDataOptThreadsExpiry(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	captured := struct {
		Symbol     string
		Expiration string
	}{}

	fake := &fakeOIBroker{
		bars: makeBars(30),
		onGetOptionChainWithOI: func(_ context.Context, symbol, expiration string) (*model.OptionChain, error) {
			captured.Symbol = symbol
			captured.Expiration = expiration
			return &model.OptionChain{Underlying: symbol}, nil
		},
	}
	svc := NewMarketDataService(fake, store)

	_, err = FetchSymbolDataOpt(context.Background(), "GOOGL", svc, FetchOptions{
		WithOI: true,
		Expiry: "20260522",
	})
	if err != nil {
		t.Fatalf("FetchSymbolDataOpt: %v", err)
	}
	if captured.Symbol != "GOOGL" || captured.Expiration != "20260522" {
		t.Errorf("got (%q, %q), want (GOOGL, 20260522)", captured.Symbol, captured.Expiration)
	}
}

func TestFetchSymbolDataOptEmptyExpiryPreservesBehavior(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	captured := struct {
		Expiration string
		Called     bool
	}{}

	fake := &fakeOIBroker{
		bars: makeBars(30),
		onGetOptionChainWithOI: func(_ context.Context, symbol, expiration string) (*model.OptionChain, error) {
			captured.Expiration = expiration
			captured.Called = true
			return &model.OptionChain{Underlying: symbol}, nil
		},
	}
	svc := NewMarketDataService(fake, store)

	_, err = FetchSymbolDataOpt(context.Background(), "GOOGL", svc, FetchOptions{
		WithOI: true,
		// Expiry intentionally left empty
	})
	if err != nil {
		t.Fatalf("FetchSymbolDataOpt: %v", err)
	}
	if !captured.Called {
		t.Fatalf("GetOptionChainWithOI was not called")
	}
	if captured.Expiration != "" {
		t.Errorf("got expiration=%q, want empty (nearest) for backwards-compat", captured.Expiration)
	}
}

func TestFetchSymbolDataOptPropagatesErrExpiryNotAvailable(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	fake := &fakeOIBroker{
		bars: makeBars(30),
		onGetOptionChainWithOI: func(_ context.Context, symbol, expiration string) (*model.OptionChain, error) {
			return nil, &broker.ErrExpiryNotAvailable{
				Underlying: symbol,
				Requested:  expiration,
				Available:  []string{"20260520", "20260522"},
			}
		},
	}
	svc := NewMarketDataService(fake, store)

	_, err = FetchSymbolDataOpt(context.Background(), "GOOGL", svc, FetchOptions{
		WithOI: true,
		Expiry: "20260523",
	})
	var miss *broker.ErrExpiryNotAvailable
	if !errors.As(err, &miss) {
		t.Fatalf("expected *broker.ErrExpiryNotAvailable, got %v", err)
	}
	if miss.Requested != "20260523" || len(miss.Available) != 2 {
		t.Errorf("unexpected error fields: %+v", miss)
	}
}
