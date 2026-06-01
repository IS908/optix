package server

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/pkg/model"
)

// MarketDataService provides market data to CLI and other consumers.
type MarketDataService struct {
	broker broker.Broker
	store  *sqlite.Store
}

// NewMarketDataService creates a new market data service.
func NewMarketDataService(b broker.Broker, s *sqlite.Store) *MarketDataService {
	return &MarketDataService{
		broker: b,
		store:  s,
	}
}

// GetQuote fetches a live quote from the broker and caches it.
func (svc *MarketDataService) GetQuote(ctx context.Context, symbol string) (*model.StockQuote, error) {
	q, err := svc.broker.GetQuote(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("get quote for %s: %w", symbol, err)
	}

	// Cache to SQLite
	if err := svc.store.UpsertStockQuote(ctx, q); err != nil {
		// Log but don't fail - quote is still valid
		fmt.Fprintf(os.Stderr, "warning: failed to cache quote: %v\n", err)
	}

	return q, nil
}

// GetHistoricalBars fetches historical bars, using cache when available.
// Cache is considered stale if the most recent bar is older than 2 calendar days
// (accounts for weekends: Friday's bar is still valid on Sunday).
func (svc *MarketDataService) GetHistoricalBars(ctx context.Context, symbol, timeframe string, days int) ([]model.OHLCV, error) {
	// Try cache first. The old predicate `len(bars) >= days` was structurally
	// impossible for the canonical 365-day request — US markets only have
	// ~252 trading days/year, so the cache was permanently bypassed. Now we
	// gate purely on freshness; the caller's downstream "≥ N bars" check
	// (e.g. fetchSymbolDataInternal requires 20) handles the
	// degenerate "cache has very few bars" case. See #39.
	bars, err := svc.store.GetBars(ctx, symbol, timeframe, days)
	if err == nil && len(bars) > 0 {
		// Check freshness: most recent bar should be within last 2 calendar days
		latest := bars[len(bars)-1].Timestamp
		if time.Since(latest) < 48*time.Hour {
			return bars, nil
		}
		// Cache is stale — fall through to broker fetch
	}

	// Fetch from broker — pass empty start/end so IB uses its defaults (~1 year)
	fresh, err := svc.broker.GetHistoricalBars(ctx, symbol, timeframe, "", "")
	if err != nil {
		// If broker fetch fails but we have cached data, return stale cache
		// rather than failing entirely (stale data is better than no data).
		if len(bars) > 0 {
			return bars, nil
		}
		return nil, fmt.Errorf("get historical bars for %s: %w", symbol, err)
	}

	// Cache
	if err := svc.store.InsertBars(ctx, symbol, timeframe, fresh); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to cache bars: %v\n", err)
	}

	return fresh, nil
}

// GetOptionChain fetches the option chain from the broker and caches OI data.
func (svc *MarketDataService) GetOptionChain(ctx context.Context, underlying, expiration string) (*model.OptionChain, error) {
	chain, err := svc.broker.GetOptionChain(ctx, underlying, expiration)
	if err != nil {
		return nil, fmt.Errorf("get option chain for %s: %w", underlying, err)
	}

	if err := svc.backfillOptionChainUnderlyingPrice(ctx, underlying, chain); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to backfill option chain underlying price: %v\n", err)
	}

	// Cache to option_quotes so freshness tracking picks it up
	if err := svc.store.UpsertOptionChain(ctx, chain); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to cache option chain: %v\n", err)
	}

	return chain, nil
}

// GetOptionChainWithOI fetches the option chain enriched with per-contract
// Open Interest. Requires the broker to implement broker.OIFetcher.
// Returns broker.ErrOINotSupported when the active data source cannot
// provide OI.
func (svc *MarketDataService) GetOptionChainWithOI(ctx context.Context, underlying, expiration string) (*model.OptionChain, error) {
	f, ok := svc.broker.(broker.OIFetcher)
	if !ok {
		return nil, broker.ErrOINotSupported
	}
	chain, err := f.GetOptionChainWithOI(ctx, underlying, expiration)
	if err != nil {
		return nil, fmt.Errorf("get option chain with OI for %s: %w", underlying, err)
	}
	if err := svc.backfillOptionChainUnderlyingPrice(ctx, underlying, chain); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to backfill option chain underlying price: %v\n", err)
	}
	if err := svc.store.UpsertOptionChain(ctx, chain); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to cache option chain: %v\n", err)
	}
	return chain, nil
}

func (svc *MarketDataService) backfillOptionChainUnderlyingPrice(ctx context.Context, underlying string, chain *model.OptionChain) error {
	if chain == nil || chain.UnderlyingPrice > 0 {
		return nil
	}
	q, err := svc.broker.GetQuote(ctx, underlying)
	if err != nil {
		return err
	}
	if q == nil {
		return nil
	}
	spot := q.Last
	if spot <= 0 && q.Bid > 0 && q.Ask > 0 {
		spot = (q.Bid + q.Ask) / 2
	}
	if spot <= 0 {
		spot = q.Close
	}
	if spot > 0 {
		chain.UnderlyingPrice = spot
	}
	return nil
}
