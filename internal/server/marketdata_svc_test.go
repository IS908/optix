package server

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/pkg/model"
)

// fakeBarsBroker is a minimal broker.Broker that records GetHistoricalBars
// calls and lets the test control the returned bars. We embed nothing — the
// methods exist as bare stubs so tests don't accidentally depend on behavior
// from a richer fake.
type fakeBarsBroker struct {
	barsToReturn []model.OHLCV
	barsCalled   bool
	barsErr      error
	quoteCalls   int
	quoteLast    float64
	quoteBid     float64
	quoteAsk     float64
	quoteClose   float64
	oiChain      *model.OptionChain
}

func (f *fakeBarsBroker) Connect(context.Context) error { return nil }
func (f *fakeBarsBroker) Disconnect() error             { return nil }
func (f *fakeBarsBroker) IsConnected() bool             { return true }

func (f *fakeBarsBroker) GetQuote(_ context.Context, sym string) (*model.StockQuote, error) {
	f.quoteCalls++
	last := f.quoteLast
	if last <= 0 && f.quoteBid <= 0 && f.quoteAsk <= 0 && f.quoteClose <= 0 {
		last = 100
	}
	return &model.StockQuote{Symbol: sym, Last: last, Bid: f.quoteBid, Ask: f.quoteAsk, Close: f.quoteClose}, nil
}

func (f *fakeBarsBroker) GetHistoricalBars(_ context.Context, _, _, _, _ string) ([]model.OHLCV, error) {
	f.barsCalled = true
	if f.barsErr != nil {
		return nil, f.barsErr
	}
	return f.barsToReturn, nil
}

func (f *fakeBarsBroker) GetOptionChain(context.Context, string, string) (*model.OptionChain, error) {
	return nil, nil
}

func (f *fakeBarsBroker) GetOptionChainWithOI(context.Context, string, string) (*model.OptionChain, error) {
	return f.oiChain, nil
}

func TestGetOptionChainWithOIBackfillsUnderlyingPriceFromQuote(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	fake := &fakeBarsBroker{
		quoteLast: 180,
		oiChain: &model.OptionChain{
			Underlying: "GOOGL",
			Expirations: []model.OptionChainExpiry{{
				Expiration: "20260515",
				Calls: []model.OptionQuote{{
					Underlying: "GOOGL", Expiration: "20260515", Strike: 185,
					OptionType: model.OptionTypeCall, ImpliedVolatility: 0.31,
				}},
			}},
		},
	}
	svc := NewMarketDataService(fake, store)

	chain, err := svc.GetOptionChainWithOI(context.Background(), "GOOGL", "20260515")
	if err != nil {
		t.Fatalf("GetOptionChainWithOI: %v", err)
	}
	if chain.UnderlyingPrice != 180 {
		t.Fatalf("UnderlyingPrice = %v, want quote-backed 180", chain.UnderlyingPrice)
	}
}

func TestGetOptionChainWithOIBackfillsUnderlyingPriceFromBidAskWhenLastMissing(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	fake := &fakeBarsBroker{
		quoteBid:   179,
		quoteAsk:   181,
		quoteClose: 175,
		oiChain: &model.OptionChain{
			Underlying:  "GOOGL",
			Expirations: []model.OptionChainExpiry{{Expiration: "20260515"}},
		},
	}
	svc := NewMarketDataService(fake, store)

	chain, err := svc.GetOptionChainWithOI(context.Background(), "GOOGL", "20260515")
	if err != nil {
		t.Fatalf("GetOptionChainWithOI: %v", err)
	}
	if chain.UnderlyingPrice != 180 {
		t.Fatalf("UnderlyingPrice = %v, want bid/ask midpoint 180", chain.UnderlyingPrice)
	}
}

func TestGetOptionChainWithOIDoesNotBackfillWhenBrokerAlreadySetSpot(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	fake := &fakeBarsBroker{
		quoteLast: 999,
		oiChain: &model.OptionChain{
			Underlying:      "GOOGL",
			UnderlyingPrice: 180,
			Expirations:     []model.OptionChainExpiry{{Expiration: "20260515"}},
		},
	}
	svc := NewMarketDataService(fake, store)

	chain, err := svc.GetOptionChainWithOI(context.Background(), "GOOGL", "20260515")
	if err != nil {
		t.Fatalf("GetOptionChainWithOI: %v", err)
	}
	if chain.UnderlyingPrice != 180 {
		t.Fatalf("UnderlyingPrice = %v, want broker-provided 180", chain.UnderlyingPrice)
	}
	if fake.quoteCalls != 0 {
		t.Fatalf("quote calls = %d, want 0 when chain already has spot", fake.quoteCalls)
	}
}

// freshBars returns n synthetic daily bars whose latest timestamp is `now`.
func freshBars(n int, now time.Time) []model.OHLCV {
	bars := make([]model.OHLCV, n)
	day := now.Truncate(24 * time.Hour)
	for i := 0; i < n; i++ {
		bars[i] = model.OHLCV{
			Timestamp: day.AddDate(0, 0, -(n - 1 - i)),
			Open:      100, High: 101, Low: 99, Close: 100, Volume: 1000,
		}
	}
	return bars
}

// TestGetHistoricalBars_CacheHitWhenFresh locks the contract for #39: when
// the SQLite cache holds enough fresh bars, GetHistoricalBars must serve
// from cache and NOT call the broker.
//
// Pre-fix: the predicate `len(bars) >= days` only held when the cache had
// ≥ 365 daily bars for a 365-day request. US markets have ~252 trading
// days/year, so the predicate was structurally impossible and the broker
// was hit on every call.
func TestGetHistoricalBars_CacheHitWhenFresh(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	// Seed the store with 252 fresh trading-day bars (a full US trading year).
	const cachedCount = 252
	now := time.Now().UTC()
	seeded := freshBars(cachedCount, now)
	if err := store.InsertBars(ctx, "AAPL", "1 day", seeded); err != nil {
		t.Fatalf("seed bars: %v", err)
	}

	fake := &fakeBarsBroker{
		barsErr: fmt.Errorf("broker should not be called when cache is fresh"),
	}
	svc := NewMarketDataService(fake, store)

	// Canonical call shape from FetchSymbolData — 365-day daily bars.
	bars, err := svc.GetHistoricalBars(ctx, "AAPL", "1 day", 365)
	if err != nil {
		t.Fatalf("GetHistoricalBars: %v", err)
	}
	if fake.barsCalled {
		t.Fatal("broker GetHistoricalBars was called; cache should have short-circuited")
	}
	if len(bars) == 0 {
		t.Fatalf("expected cached bars, got 0")
	}
}

// TestGetHistoricalBars_BrokerCalledWhenStale verifies the stale-cache path:
// cache holds bars but the most recent is older than 48h → broker is called.
func TestGetHistoricalBars_BrokerCalledWhenStale(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	// Seed with bars whose latest is 3 days ago.
	threeDaysAgo := time.Now().UTC().Add(-72 * time.Hour)
	stale := freshBars(252, threeDaysAgo)
	if err := store.InsertBars(ctx, "AAPL", "1 day", stale); err != nil {
		t.Fatalf("seed bars: %v", err)
	}

	freshFromBroker := freshBars(252, time.Now().UTC())
	fake := &fakeBarsBroker{barsToReturn: freshFromBroker}
	svc := NewMarketDataService(fake, store)

	_, err = svc.GetHistoricalBars(ctx, "AAPL", "1 day", 365)
	if err != nil {
		t.Fatalf("GetHistoricalBars: %v", err)
	}
	if !fake.barsCalled {
		t.Fatal("broker GetHistoricalBars was NOT called; stale cache should have triggered a fresh fetch")
	}
}

// TestGetHistoricalBars_BrokerErrorFallsBackToStaleCache verifies that if
// the broker errors AND a cache exists (even stale), we serve the cache
// rather than returning an error. This is the existing graceful-degradation
// path on marketdata_svc.go:62-66 — pin it so a future refactor can't
// silently regress.
func TestGetHistoricalBars_BrokerErrorFallsBackToStaleCache(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	threeDaysAgo := time.Now().UTC().Add(-72 * time.Hour)
	stale := freshBars(252, threeDaysAgo)
	if err := store.InsertBars(ctx, "AAPL", "1 day", stale); err != nil {
		t.Fatalf("seed bars: %v", err)
	}

	fake := &fakeBarsBroker{barsErr: fmt.Errorf("broker down")}
	svc := NewMarketDataService(fake, store)

	bars, err := svc.GetHistoricalBars(ctx, "AAPL", "1 day", 365)
	if err != nil {
		t.Fatalf("expected graceful fallback to stale cache, got error: %v", err)
	}
	if !fake.barsCalled {
		t.Fatal("broker should have been called (cache was stale)")
	}
	if len(bars) == 0 {
		t.Fatal("expected stale cache returned despite broker error, got 0 bars")
	}
}
