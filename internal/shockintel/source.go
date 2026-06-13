package shockintel

import (
	"context"
	"fmt"
	"strings"
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
	connect  BrokerConnector
	fallback Source
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
	return &BrokerQuoteAdapter{connect: connect, fallback: fallback}
}

func NewYFinanceAdapter(pythonBin string) *YFinanceAdapter {
	return &YFinanceAdapter{src: marketdata.NewYFinanceSource(pythonBin)}
}

func (a *BrokerQuoteAdapter) Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error) {
	out := map[string]ShockQuote{}
	var fallbackErr error
	if a.fallback != nil {
		fallbackQuotes, err := a.fallback.Quotes(ctx, ids)
		if err != nil {
			fallbackErr = err
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

	b, source, err := a.connect(ctx)
	if err != nil {
		if len(out) > 0 {
			return out, nil
		}
		if fallbackErr != nil {
			return nil, fmt.Errorf("broker quotes: %w; fallback: %v", err, fallbackErr)
		}
		return nil, fmt.Errorf("broker quotes: %w", err)
	}
	source = normalizeSourceName(source)
	defer b.Disconnect()

	var quoteErrs []string
	for _, id := range brokerIDs {
		q, err := b.GetQuote(ctx, id)
		if err != nil {
			quoteErrs = append(quoteErrs, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		out[id] = stockQuoteToShock(id, q, source)
	}
	if len(out) == 0 && len(quoteErrs) > 0 {
		return nil, fmt.Errorf("broker quotes: %s", strings.Join(quoteErrs, "; "))
	}
	return out, nil
}

func (a *BrokerQuoteAdapter) Bars(ctx context.Context, ids []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error) {
	if a.fallback == nil {
		return map[string][]model.OHLCV{}, fmt.Errorf("bars unavailable: no fallback source")
	}
	return a.fallback.Bars(ctx, ids, interval, lookback)
}

func (a *BrokerQuoteAdapter) Depth(context.Context, []string, int) (map[string]DepthSnapshot, error) {
	return map[string]DepthSnapshot{}, fmt.Errorf("market depth unavailable in broker quote adapter; IBKR top-of-book bid/ask is used when available")
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
