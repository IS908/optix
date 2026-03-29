package webui

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/IS908/optix/internal/server"
	"github.com/IS908/optix/internal/watchlist"
	"github.com/IS908/optix/pkg/model"
)

// ── Response types ───────────────────────────────────────────────────────────

// QuoteTickerItem is one symbol's real-time quote for the ticker zone.
type QuoteTickerItem struct {
	Symbol        string  `json:"symbol"`
	Last          float64 `json:"last"`
	Change        float64 `json:"change"`
	ChangePct     float64 `json:"change_pct"`
	Bid           float64 `json:"bid"`
	Ask           float64 `json:"ask"`
	Volume        int64   `json:"volume"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	Open          float64 `json:"open"`
	Close         float64 `json:"close"`           // previous close
	MarketSession string  `json:"market_session"`   // "pre_market" | "regular" | "post_market" | "closed"
	SessionLabel  string  `json:"session_label"`    // 盘前 | 盘中 | 盘后 | 休市
}

// QuoteTickerResponse is the payload for GET /api/quotes and /api/quote/{symbol}.
type QuoteTickerResponse struct {
	Quotes        []QuoteTickerItem `json:"quotes"`
	MarketSession string            `json:"market_session"`
	SessionLabel  string            `json:"session_label"`
	ServerTime    time.Time         `json:"server_time"`
}

// ── Quote cache with TTL ─────────────────────────────────────────────────────

// quoteCache holds the most recent /api/quotes result with a short TTL.
// This prevents every 30-second poll from hitting the broker, which can take
// 10+ seconds per request (especially via Yahoo Finance fallback).
type quoteCache struct {
	mu     sync.RWMutex
	data   *QuoteTickerResponse
	expiry time.Time
}

// quoteCacheTTL controls how long cached quotes are considered fresh.
// The ticker zone polls every 30 seconds, so a 10-second TTL means:
//   - First request in a cycle: fetches from broker (~10s), caches result
//   - Subsequent requests within 10s: served from cache (~0ms)
//   - After TTL expires: next request triggers a fresh fetch
const quoteCacheTTL = 10 * time.Second

func (c *quoteCache) get() *QuoteTickerResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.data != nil && time.Now().Before(c.expiry) {
		return c.data
	}
	return nil
}

func (c *quoteCache) set(resp *QuoteTickerResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = resp
	c.expiry = time.Now().Add(quoteCacheTTL)
}

// ── Handlers ─────────────────────────────────────────────────────────────────

// handleAPIQuotes returns lightweight real-time quotes for all watchlist symbols.
// Results are cached for 10 seconds to avoid broker round-trips on every poll.
func (s *Server) handleAPIQuotes(w http.ResponseWriter, r *http.Request) {
	resp, err := s.fetchQuickQuotes(r.Context())
	if err != nil {
		writeErrorJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, resp)
}

// handleAPIQuoteSingle returns a single symbol's real-time quote.
// Used by the analyze page ticker card to avoid fetching all watchlist quotes.
func (s *Server) handleAPIQuoteSingle(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	if symbol == "" {
		writeErrorJSON(w, "symbol is required", http.StatusBadRequest)
		return
	}

	// Try to serve from the batch cache first — zero cost if available.
	if cached := s.qCache.get(); cached != nil {
		for _, q := range cached.Quotes {
			if q.Symbol == symbol {
				writeJSON(w, &QuoteTickerResponse{
					Quotes:        []QuoteTickerItem{q},
					MarketSession: cached.MarketSession,
					SessionLabel:  cached.SessionLabel,
					ServerTime:    cached.ServerTime,
				})
				return
			}
		}
	}

	// Cache miss — fetch just this one symbol.
	resp, err := s.fetchSingleQuote(r.Context(), symbol)
	if err != nil {
		writeErrorJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, resp)
}

// ── Data fetching ────────────────────────────────────────────────────────────

// fetchQuickQuotes returns cached quotes if fresh, otherwise deduplicates
// concurrent broker fetches via singleflight.
func (s *Server) fetchQuickQuotes(ctx context.Context) (*QuoteTickerResponse, error) {
	// Fast path: serve from TTL cache.
	if cached := s.qCache.get(); cached != nil {
		return cached, nil
	}

	// Slow path: fetch from broker (deduplicated).
	v, err, _ := s.sfGroup.Do("quotes", func() (any, error) {
		resp, fetchErr := s.doFetchQuickQuotes(ctx)
		if fetchErr == nil {
			s.qCache.set(resp)
		}
		return resp, fetchErr
	})
	if err != nil {
		return nil, err
	}
	return v.(*QuoteTickerResponse), nil
}

// fetchSingleQuote fetches a quote for one symbol with singleflight dedup.
func (s *Server) fetchSingleQuote(ctx context.Context, symbol string) (*QuoteTickerResponse, error) {
	v, err, _ := s.sfGroup.Do("quote:"+symbol, func() (any, error) {
		return s.doFetchSingleQuote(ctx, symbol)
	})
	if err != nil {
		return nil, err
	}
	return v.(*QuoteTickerResponse), nil
}

// doFetchQuickQuotes fetches quotes for all watchlist symbols concurrently.
// Only calls broker.GetQuote — no bars, options, or analysis.
func (s *Server) doFetchQuickQuotes(ctx context.Context) (*QuoteTickerResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	session := model.USMarketSession(time.Now())

	mgr := watchlist.NewManager(s.store)
	items, err := mgr.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("get watchlist: %w", err)
	}
	if len(items) == 0 {
		return &QuoteTickerResponse{
			Quotes:        []QuoteTickerItem{},
			MarketSession: string(session),
			SessionLabel:  session.Label(),
			ServerTime:    time.Now().UTC(),
		}, nil
	}

	conn, err := s.brokerPool.acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect to broker: %w", err)
	}
	healthy := true
	defer func() { s.brokerPool.release(conn, healthy) }()

	svc := server.NewMarketDataService(conn.b, s.store)

	// Fetch quotes concurrently (max 5 at a time).
	type result struct {
		idx  int
		item QuoteTickerItem
		ok   bool
	}
	results := make(chan result, len(items))
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		go func(idx int, sym string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			symCtx, symCancel := context.WithTimeout(ctx, 10*time.Second)
			defer symCancel()

			q, qErr := svc.GetQuote(symCtx, sym)
			if qErr != nil {
				results <- result{idx: idx, ok: false}
				return
			}
			results <- result{
				idx:  idx,
				item: modelQuoteToTickerItem(q),
				ok:   true,
			}
		}(i, item.Symbol)
	}
	go func() { wg.Wait(); close(results) }()

	// Collect in original watchlist order.
	ordered := make([]QuoteTickerItem, len(items))
	mask := make([]bool, len(items))
	for r := range results {
		if r.ok {
			ordered[r.idx] = r.item
			mask[r.idx] = true
		}
	}
	quotes := make([]QuoteTickerItem, 0, len(items))
	for i, ok := range mask {
		if ok {
			quotes = append(quotes, ordered[i])
		}
	}

	return &QuoteTickerResponse{
		Quotes:        quotes,
		MarketSession: string(session),
		SessionLabel:  session.Label(),
		ServerTime:    time.Now().UTC(),
	}, nil
}

// doFetchSingleQuote fetches a quote for a single symbol.
func (s *Server) doFetchSingleQuote(ctx context.Context, symbol string) (*QuoteTickerResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	session := model.USMarketSession(time.Now())

	conn, err := s.brokerPool.acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect to broker: %w", err)
	}
	healthy := true
	defer func() { s.brokerPool.release(conn, healthy) }()

	svc := server.NewMarketDataService(conn.b, s.store)
	q, err := svc.GetQuote(ctx, symbol)
	if err != nil {
		healthy = conn.isConnected()
		return nil, fmt.Errorf("get quote %s: %w", symbol, err)
	}

	return &QuoteTickerResponse{
		Quotes:        []QuoteTickerItem{modelQuoteToTickerItem(q)},
		MarketSession: string(session),
		SessionLabel:  session.Label(),
		ServerTime:    time.Now().UTC(),
	}, nil
}

// ── Conversion helper ────────────────────────────────────────────────────────

func modelQuoteToTickerItem(q *model.StockQuote) QuoteTickerItem {
	return QuoteTickerItem{
		Symbol:        q.Symbol,
		Last:          q.Last,
		Change:        q.Change,
		ChangePct:     q.ChangePct,
		Bid:           q.Bid,
		Ask:           q.Ask,
		Volume:        q.Volume,
		High:          q.High,
		Low:           q.Low,
		Open:          q.Open,
		Close:         q.Close,
		MarketSession: string(q.MarketSession),
		SessionLabel:  q.MarketSession.Label(),
	}
}
