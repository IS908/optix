package shockintel

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

type Source interface {
	Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error)
	Bars(ctx context.Context, ids []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error)
	Depth(ctx context.Context, ids []string, levels int) (map[string]DepthSnapshot, error)
	OptionMetrics(ctx context.Context, underlyings []string) (map[string]OptionStress, error)
}

type BrokerConnector func(ctx context.Context) (broker.Broker, string, error)

type BrokerQuoteAdapter struct {
	connect        BrokerConnector
	fallback       Source
	overlayTimeout time.Duration
}

type YFinanceAdapter struct {
	src *marketdata.YFinanceSource
}

var shockBrokerClientID int64 = 8700

func NewIBKRPreferredService(host string, port int, pythonBin string) *Service {
	return NewService(NewIBKRPreferredSource(host, port, pythonBin))
}

func NewIBKRPreferredSource(host string, port int, pythonBin string) *BrokerQuoteAdapter {
	fallback := NewYFinanceAdapter(pythonBin)
	return NewBrokerQuoteAdapter(func(ctx context.Context) (broker.Broker, string, error) {
		clientID := atomic.AddInt64(&shockBrokerClientID, 1)
		b := factory.NewWithFallback(ibkr.Config{Host: host, Port: port, ClientID: clientID}, pythonBin)
		if err := b.Connect(ctx); err != nil {
			return nil, "", err
		}
		return b, normalizeSourceName(b.SourceName()), nil
	}, fallback)
}

func NewBrokerQuoteAdapter(connect BrokerConnector, fallback Source) *BrokerQuoteAdapter {
	return &BrokerQuoteAdapter{connect: connect, fallback: fallback, overlayTimeout: 1500 * time.Millisecond}
}

func NewYFinanceAdapter(pythonBin string) *YFinanceAdapter {
	return &YFinanceAdapter{src: marketdata.NewYFinanceSource(pythonBin)}
}

func (a *BrokerQuoteAdapter) Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error) {
	out := map[string]ShockQuote{}
	var fallbackErr error
	if a.fallback != nil {
		fallbackCtx := ctx
		var cancelFallback context.CancelFunc
		if a.overlayTimeout > 0 {
			fallbackCtx, cancelFallback = context.WithTimeout(ctx, a.overlayTimeout)
		}
		fallbackQuotes, err := a.fallback.Quotes(fallbackCtx, ids)
		if cancelFallback != nil {
			cancelFallback()
		}
		if err != nil {
			fallbackErr = fmt.Errorf("fallback quotes degraded: %w", err)
		} else {
			for id, q := range fallbackQuotes {
				out[id] = q
			}
		}
	}

	brokerIDs := brokerQuoteIDs(ids)
	if len(brokerIDs) == 0 || a.connect == nil {
		if len(out) > 0 {
			return out, nil
		}
		if fallbackErr != nil {
			return nil, fallbackErr
		}
		return out, nil
	}

	overlayCtx := ctx
	var cancelOverlay context.CancelFunc
	if (len(out) > 0 || fallbackErr != nil) && a.overlayTimeout > 0 {
		overlayCtx, cancelOverlay = context.WithTimeout(ctx, a.overlayTimeout)
		defer cancelOverlay()
	}

	b, source, err := a.connect(overlayCtx)
	if err != nil {
		if len(out) > 0 {
			if fallbackErr != nil {
				return out, fmt.Errorf("%v; broker quotes degraded: %w", fallbackErr, err)
			}
			return out, fmt.Errorf("broker quotes degraded: %w", err)
		}
		if fallbackErr != nil {
			return nil, fmt.Errorf("broker quotes: %w; %v", err, fallbackErr)
		}
		return nil, fmt.Errorf("broker quotes: %w", err)
	}
	source = normalizeSourceName(source)
	defer b.Disconnect()

	var warnParts []string
	if fallbackErr != nil {
		warnParts = append(warnParts, fallbackErr.Error())
	}
	if source != "ibkr" {
		warnParts = append(warnParts, fmt.Sprintf("broker quotes degraded: using %s fallback for broker-preferred quotes", source))
	}
	var quoteErrs []string
	for _, id := range brokerIDs {
		q, err := b.GetQuote(overlayCtx, id)
		if err != nil {
			quoteErrs = append(quoteErrs, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		out[id] = stockQuoteToShock(id, q, source)
	}
	if len(quoteErrs) > 0 {
		warnParts = append(warnParts, fmt.Sprintf("broker quotes partial: %s", strings.Join(quoteErrs, "; ")))
	}
	if len(warnParts) > 0 {
		err := fmt.Errorf("%s", strings.Join(warnParts, "; "))
		if len(out) == 0 {
			return nil, err
		}
		return out, err
	}
	return out, nil
}

func (a *BrokerQuoteAdapter) Bars(ctx context.Context, ids []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error) {
	if a.fallback == nil {
		return map[string][]model.OHLCV{}, fmt.Errorf("bars unavailable: no fallback source")
	}
	return a.fallback.Bars(ctx, ids, interval, lookback)
}

func (a *BrokerQuoteAdapter) Depth(ctx context.Context, ids []string, levels int) (map[string]DepthSnapshot, error) {
	out := map[string]DepthSnapshot{}
	brokerIDs := brokerQuoteIDs(ids)
	if len(brokerIDs) == 0 || a.connect == nil {
		return out, nil
	}
	if levels <= 0 {
		levels = 5
	}

	depthCtx := ctx
	var cancelDepth context.CancelFunc
	if a.overlayTimeout > 0 {
		depthCtx, cancelDepth = context.WithTimeout(ctx, a.overlayTimeout)
		defer cancelDepth()
	}

	b, source, err := a.connect(depthCtx)
	if err != nil {
		return out, fmt.Errorf("broker depth degraded: %w", err)
	}
	source = normalizeSourceName(source)
	defer b.Disconnect()
	if source != "ibkr" {
		return out, fmt.Errorf("broker depth degraded: using %s fallback; IBKR market depth unavailable", source)
	}

	fetcher, ok := b.(broker.MarketDepthFetcher)
	if !ok {
		return out, broker.ErrMarketDepthNotSupported
	}

	type depthResult struct {
		id    string
		depth *model.MarketDepth
		err   error
	}
	results := make(chan depthResult, len(brokerIDs))
	var wg sync.WaitGroup
	for _, id := range brokerIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			depth, err := fetcher.GetMarketDepth(depthCtx, id, levels)
			results <- depthResult{id: id, depth: depth, err: err}
		}(id)
	}
	wg.Wait()
	close(results)

	var warnParts []string
	for result := range results {
		if result.err != nil {
			warnParts = append(warnParts, fmt.Sprintf("%s: %v", result.id, result.err))
			continue
		}
		out[result.id] = marketDepthToShock(result.id, result.depth, source)
	}
	if len(warnParts) > 0 {
		err := fmt.Errorf("broker depth partial: %s", strings.Join(warnParts, "; "))
		if len(out) == 0 {
			return out, err
		}
		return out, err
	}
	return out, nil
}

func (a *BrokerQuoteAdapter) OptionMetrics(context.Context, []string) (map[string]OptionStress, error) {
	return map[string]OptionStress{}, fmt.Errorf("option stress unavailable in M7 v1; use IBKR OPRA chain/OI integration in a follow-up")
}

func (a *YFinanceAdapter) Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error) {
	quotes, err := a.src.BatchQuotes(ctx, shockRefsForIDs(ids))
	if err != nil {
		return nil, err
	}
	out := make(map[string]ShockQuote, len(quotes))
	for id, q := range quotes {
		out[id] = ShockQuote{
			ID: id, Label: q.Label, Price: q.Price, Change: q.Change, ChangePct: q.ChangePct,
			Source: "yfinance", Basis: string(q.Basis), AsOf: q.AsOf.UTC(),
		}
	}
	return out, nil
}

func (a *YFinanceAdapter) Bars(ctx context.Context, ids []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error) {
	return a.src.BatchBars(ctx, shockRefsForIDs(ids), interval, lookback)
}

func (a *YFinanceAdapter) Depth(context.Context, []string, int) (map[string]DepthSnapshot, error) {
	return map[string]DepthSnapshot{}, fmt.Errorf("depth unavailable from yfinance fallback; use IBKR market depth for realtime liquidity")
}

func (a *YFinanceAdapter) OptionMetrics(context.Context, []string) (map[string]OptionStress, error) {
	return map[string]OptionStress{}, fmt.Errorf("option stress unavailable from yfinance fallback in M7 v1; use IBKR OPRA data for IV/Greeks/OI/volume")
}

func shockRefsForIDs(ids []string) []marketdata.AssetRef {
	classes := shockAssetClasses()
	refs := make([]marketdata.AssetRef, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		class, ok := classes[id]
		if !ok {
			continue
		}
		refs = append(refs, marketdata.AssetRef{ID: id, Class: class})
		seen[id] = struct{}{}
	}
	return refs
}

func shockAssetClasses() map[string]marketdata.AssetClass {
	return map[string]marketdata.AssetClass{
		"SPY": marketdata.ClassStock, "QQQ": marketdata.ClassStock,
		"IWM": marketdata.ClassStock, "TLT": marketdata.ClassStock,
		"HYG": marketdata.ClassStock, "LQD": marketdata.ClassStock,
		"GLD": marketdata.ClassStock, "USO": marketdata.ClassStock,
		"UUP": marketdata.ClassStock, "VIXY": marketdata.ClassStock,
		"VIX":   marketdata.ClassIndex,
		"US10Y": marketdata.ClassYield,
		"ES":    marketdata.ClassFuture, "NQ": marketdata.ClassFuture,
		"RTY_F": marketdata.ClassFuture, "YM": marketdata.ClassFuture,
		"WTI": marketdata.ClassFuture, "GOLD": marketdata.ClassFuture,
	}
}

func brokerQuoteIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		if !brokerQuoteSupported(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		out = append(out, id)
		seen[id] = struct{}{}
	}
	return out
}

func brokerQuoteSupported(id string) bool {
	switch id {
	case "SPY", "QQQ", "IWM", "TLT", "HYG", "LQD", "GLD", "USO", "UUP", "VIXY":
		return true
	default:
		return false
	}
}

func stockQuoteToShock(id string, q *model.StockQuote, source string) ShockQuote {
	if q == nil {
		return ShockQuote{ID: id, Label: id, Source: source, Basis: basisForSource(source), AsOf: time.Now().UTC()}
	}
	return ShockQuote{
		ID: id, Label: nonEmpty(q.Symbol, id), Price: q.Last, Change: q.Change, ChangePct: q.ChangePct,
		Bid: q.Bid, Ask: q.Ask, Source: source, Basis: basisForSource(source), AsOf: q.Timestamp.UTC(),
	}
}

func marketDepthToShock(id string, depth *model.MarketDepth, source string) DepthSnapshot {
	out := DepthSnapshot{ID: id, Source: source, Basis: basisForSource(source), AsOf: time.Now().UTC()}
	if depth == nil {
		out.Missing = true
		out.Note = "depth missing"
		return out
	}
	if !depth.Timestamp.IsZero() {
		out.AsOf = depth.Timestamp.UTC()
	}
	for _, level := range depth.Levels {
		if level.Side != "bid" && level.Side != "ask" {
			continue
		}
		out.Levels = append(out.Levels, DepthLevel{
			Side:  level.Side,
			Price: level.Price,
			Size:  level.Size,
		})
	}
	if len(out.Levels) == 0 {
		out.Missing = true
		out.Note = "depth missing"
	}
	return out
}

func normalizeSourceName(name string) string {
	low := strings.ToLower(name)
	switch {
	case strings.Contains(low, "ibkr"):
		return "ibkr"
	case strings.Contains(low, "yahoo") || strings.Contains(low, "yfinance"):
		return "yfinance"
	default:
		return strings.TrimSpace(name)
	}
}

func basisForSource(source string) string {
	if source == "yfinance" {
		return "delayed"
	}
	if source == "ibkr" {
		return "realtime_or_delayed"
	}
	return "unknown"
}
