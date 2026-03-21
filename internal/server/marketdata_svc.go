package server

import (
	"context"
	"fmt"

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
		fmt.Printf("warning: failed to cache quote: %v\n", err)
	}

	return q, nil
}

// GetHistoricalBars fetches historical bars, using cache when available.
func (svc *MarketDataService) GetHistoricalBars(ctx context.Context, symbol, timeframe string, days int) ([]model.OHLCV, error) {
	// Try cache first
	bars, err := svc.store.GetBars(ctx, symbol, timeframe, days)
	if err == nil && len(bars) >= days {
		return bars, nil
	}

	// Fetch from broker — pass empty start/end so IB uses its defaults (~1 year)
	bars, err = svc.broker.GetHistoricalBars(ctx, symbol, timeframe, "", "")
	if err != nil {
		return nil, fmt.Errorf("get historical bars for %s: %w", symbol, err)
	}

	// Cache
	if err := svc.store.InsertBars(ctx, symbol, timeframe, bars); err != nil {
		fmt.Printf("warning: failed to cache bars: %v\n", err)
	}

	return bars, nil
}

// GetOptionChain fetches the option chain from the broker and caches OI data.
func (svc *MarketDataService) GetOptionChain(ctx context.Context, underlying, expiration string) (*model.OptionChain, error) {
	chain, err := svc.broker.GetOptionChain(ctx, underlying, expiration)
	if err != nil {
		return nil, fmt.Errorf("get option chain for %s: %w", underlying, err)
	}

	// Cache to option_quotes so freshness tracking picks it up
	if err := svc.store.UpsertOptionChain(ctx, chain); err != nil {
		fmt.Printf("warning: failed to cache option chain: %v\n", err)
	}

	return chain, nil
}
