package shockintel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

var _ MarketSource = (*fakeShockSource)(nil)

func TestServiceBundleKeepsNonNilSlices(t *testing.T) {
	svc := NewService(&fakeShockSource{})
	svc.Now = fixedShockNow

	bundle, err := svc.Bundle(context.Background())
	if err != nil {
		t.Fatalf("Bundle error = %v", err)
	}
	if bundle.Regime.Confirmations == nil || bundle.Fingerprint.Rows == nil ||
		bundle.Analogs.Rows == nil || bundle.Liquidity.Rows == nil {
		t.Fatalf("bundle contains nil slices: %#v", bundle)
	}
	if len(bundle.Fingerprint.Rows) != 4 || len(bundle.Analogs.Rows) == 0 || len(bundle.Liquidity.Rows) == 0 {
		t.Fatalf("bundle missing expected rows: %#v", bundle)
	}
}

func TestServiceDegradesOnQuoteFailure(t *testing.T) {
	svc := NewService(&fakeShockSource{quoteErr: errors.New("offline")})
	svc.Now = fixedShockNow

	bundle, err := svc.Bundle(context.Background())
	if err != nil {
		t.Fatalf("Bundle error = %v", err)
	}
	if bundle.Liquidity.Rows == nil || bundle.Fingerprint.Rows == nil || bundle.Analogs.Rows == nil {
		t.Fatalf("degraded bundle contains nil slices: %#v", bundle)
	}
	if !warningsContain(bundle.Regime.Warnings, "quotes") {
		t.Fatalf("regime warnings = %#v, want quotes warning", bundle.Regime.Warnings)
	}
	if !warningsContain(bundle.Liquidity.Warnings, "quotes") {
		t.Fatalf("liquidity warnings = %#v, want quotes warning", bundle.Liquidity.Warnings)
	}
}

func TestServiceLiquidityWarnsWhenDepthUnavailable(t *testing.T) {
	svc := NewService(&fakeShockSource{depthErr: errors.New("depth unavailable")})
	svc.Now = fixedShockNow

	dto, err := svc.Liquidity(context.Background())
	if err != nil {
		t.Fatalf("Liquidity error = %v", err)
	}
	if len(dto.Rows) == 0 {
		t.Fatal("expected liquidity rows despite depth failure")
	}
	if !warningsContain(dto.Warnings, "depth") {
		t.Fatalf("warnings = %#v, want depth warning", dto.Warnings)
	}
}

func TestServiceFingerprintIncludesOptionStressRows(t *testing.T) {
	now := fixedShockNow()
	svc := NewService(&fakeShockSource{optionMetrics: map[string]OptionStress{
		"SPY": {
			Underlying: "SPY", Source: "ibkr", Basis: string(marketdata.BasisDelayed), AsOf: now,
			IVSkew: 0.08, Volume: 2500, OpenInt: 12000, Note: "atm_iv=0.42 skew=0.08",
		},
	}})
	svc.Now = fixedShockNow

	dto, err := svc.Fingerprint(context.Background())
	if err != nil {
		t.Fatalf("Fingerprint error = %v", err)
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal fingerprint = %v", err)
	}
	var body struct {
		OptionStress []OptionStress `json:"option_stress"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal fingerprint = %v", err)
	}
	if len(body.OptionStress) != 1 {
		t.Fatalf("option_stress rows = %d, want 1; json=%s", len(body.OptionStress), string(raw))
	}
	if body.OptionStress[0].Underlying != "SPY" || body.OptionStress[0].Source != "ibkr" || body.OptionStress[0].Basis == "" {
		t.Fatalf("option_stress row missing source semantics: %#v", body.OptionStress[0])
	}
	if bytes.Contains(raw, []byte("iv_change")) {
		t.Fatalf("option_stress JSON must not expose temporal iv_change field: %s", string(raw))
	}
	if !bytes.Contains(raw, []byte("iv_skew")) {
		t.Fatalf("option_stress JSON missing iv_skew field: %s", string(raw))
	}
	if !fingerprintEvidenceContains(dto.Rows, "option IV skew elevated") {
		t.Fatalf("fingerprint rows = %#v, want option IV skew evidence", dto.Rows)
	}
}

func TestServiceBundleDedupesShockMarketFetches(t *testing.T) {
	src := &countingShockSource{inner: &fakeShockSource{}}
	svc := NewService(src)
	svc.Now = fixedShockNow

	if _, err := svc.Bundle(context.Background()); err != nil {
		t.Fatalf("Bundle error = %v", err)
	}

	if got := src.quoteFetches("SPY"); got != 1 {
		t.Fatalf("SPY quote fetches = %d, want 1", got)
	}
	if got := src.quoteFetches("VIX"); got != 1 {
		t.Fatalf("VIX quote fetches = %d, want 1", got)
	}
	if got := src.depthCalls; got != 1 {
		t.Fatalf("depth calls = %d, want 1", got)
	}
}

func TestServiceRegimeDedupesNestedLiquidityFetches(t *testing.T) {
	src := &countingShockSource{inner: &fakeShockSource{}}
	svc := NewService(src)
	svc.Now = fixedShockNow

	if _, err := svc.Regime(context.Background()); err != nil {
		t.Fatalf("Regime error = %v", err)
	}

	if got := src.quoteFetches("SPY"); got != 1 {
		t.Fatalf("SPY quote fetches = %d, want 1", got)
	}
	if got := src.depthCalls; got != 1 {
		t.Fatalf("depth calls = %d, want 1", got)
	}
}

func TestServiceCachesMarketFetchesAcrossShockCards(t *testing.T) {
	src := &countingShockSource{inner: &fakeShockSource{}}
	svc := NewService(src)
	svc.Now = fixedShockNow

	if _, err := svc.Regime(context.Background()); err != nil {
		t.Fatalf("Regime error = %v", err)
	}
	if _, err := svc.Fingerprint(context.Background()); err != nil {
		t.Fatalf("Fingerprint error = %v", err)
	}
	if _, err := svc.Analogs(context.Background()); err != nil {
		t.Fatalf("Analogs error = %v", err)
	}
	if _, err := svc.Liquidity(context.Background()); err != nil {
		t.Fatalf("Liquidity error = %v", err)
	}

	if got := src.quoteFetches("SPY"); got != 1 {
		t.Fatalf("SPY quote fetches across cards = %d, want 1", got)
	}
	if got := src.depthCalls; got != 1 {
		t.Fatalf("depth calls across cards = %d, want 1", got)
	}
}

func TestBasisForSourceUsesCanonicalMarketdataValues(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   marketdata.Basis
	}{
		{source: "yfinance", want: marketdata.BasisDelayed},
		{source: "ibkr", want: marketdata.BasisDelayed},
		{source: "", want: marketdata.BasisDelayed},
	} {
		if got := basisForSource(tc.source); got != string(tc.want) {
			t.Fatalf("basisForSource(%q) = %q, want %q", tc.source, got, tc.want)
		}
	}
}

func TestYFinanceAdapterDepthDegradesExplicitly(t *testing.T) {
	adapter := NewYFinanceAdapter("python3")

	if _, err := adapter.Depth(context.Background(), []string{"SPY"}, 5); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("Depth error = %v, want explicit depth error", err)
	}
}

func TestBrokerQuoteAdapterOptionMetricsUsesBrokerChain(t *testing.T) {
	now := fixedShockNow()
	chain := &model.OptionChain{
		Underlying:      "SPY",
		UnderlyingPrice: 500,
		Expirations: []model.OptionChainExpiry{{
			Expiration: "20260717",
			Calls: []model.OptionQuote{
				{Underlying: "SPY", Expiration: "20260717", Strike: 500, OptionType: model.OptionTypeCall, Volume: 900, OpenInterest: 5000, ImpliedVolatility: 0.34},
				{Underlying: "SPY", Expiration: "20260717", Strike: 510, OptionType: model.OptionTypeCall, Volume: 300, OpenInterest: 1500, ImpliedVolatility: 0.31},
			},
			Puts: []model.OptionQuote{
				{Underlying: "SPY", Expiration: "20260717", Strike: 500, OptionType: model.OptionTypePut, Volume: 1600, OpenInterest: 7000, ImpliedVolatility: 0.43},
				{Underlying: "SPY", Expiration: "20260717", Strike: 490, OptionType: model.OptionTypePut, Volume: 500, OpenInterest: 2600, ImpliedVolatility: 0.39},
			},
		}},
	}
	adapter := NewBrokerQuoteAdapter(
		func(context.Context) (broker.Broker, string, error) {
			return testBroker{quotes: map[string]*model.StockQuote{
				"SPY": {Symbol: "SPY", Last: 501, Timestamp: now},
			}, chains: map[string]*model.OptionChain{"SPY": chain}}, "IBKR", nil
		},
		staticFallbackSource{},
	)

	got, err := adapter.OptionMetrics(context.Background(), []string{"SPY"})
	if err != nil {
		t.Fatalf("OptionMetrics error = %v", err)
	}
	spy := got["SPY"]
	if spy.Source != "ibkr" || spy.Basis == "" || spy.AsOf.IsZero() {
		t.Fatalf("SPY source semantics = %#v", spy)
	}
	if spy.IVSkew <= 0.08 || spy.Volume != 3300 || spy.OpenInt != 16100 {
		t.Fatalf("SPY metrics = %#v, want skew/volume/OI aggregation", spy)
	}
}

func TestBrokerQuoteAdapterOverlaysBrokerQuotes(t *testing.T) {
	now := fixedShockNow()
	adapter := NewBrokerQuoteAdapter(
		func(context.Context) (broker.Broker, string, error) {
			return testBroker{quotes: map[string]*model.StockQuote{
				"SPY": {Symbol: "SPY", Last: 501, Bid: 500.9, Ask: 501.1, Change: -14, ChangePct: -2.72, Timestamp: now},
			}}, "IBKR", nil
		},
		staticFallbackSource{quotes: map[string]ShockQuote{
			"SPY":   {ID: "SPY", Label: "SPY", Price: 500, ChangePct: -2.5, Source: "yfinance", Basis: "delayed", AsOf: now},
			"VIX":   {ID: "VIX", Label: "VIX", Price: 28, ChangePct: 25, Source: "yfinance", Basis: "delayed", AsOf: now},
			"US10Y": {ID: "US10Y", Label: "US 10Y", Price: 4.4, ChangePct: 0.9, Source: "yfinance", Basis: "approx", AsOf: now},
		}},
	)

	quotes, err := adapter.Quotes(context.Background(), []string{"SPY", "VIX", "US10Y"})
	if err != nil {
		t.Fatalf("Quotes error = %v", err)
	}
	if quotes["SPY"].Source != "ibkr" || quotes["SPY"].Bid != 500.9 || quotes["SPY"].Ask != 501.1 {
		t.Fatalf("SPY not overlaid from broker: %#v", quotes["SPY"])
	}
	if quotes["VIX"].Source != "yfinance" || quotes["US10Y"].Source != "yfinance" {
		t.Fatalf("fallback macro quotes not preserved: %#v", quotes)
	}
}

func TestBrokerQuoteAdapterDepthUsesBrokerMarketDepth(t *testing.T) {
	now := fixedShockNow()
	adapter := NewBrokerQuoteAdapter(
		func(context.Context) (broker.Broker, string, error) {
			return testBroker{depth: map[string]*model.MarketDepth{
				"SPY": {
					Symbol:    "SPY",
					Timestamp: now,
					Levels: []model.MarketDepthLevel{
						{Side: "bid", Position: 0, Price: 499.9, Size: 1000},
						{Side: "bid", Position: 1, Price: 499.8, Size: 800},
						{Side: "ask", Position: 0, Price: 500.1, Size: 900},
						{Side: "ask", Position: 1, Price: 500.2, Size: 850},
					},
				},
			}}, "IBKR", nil
		},
		staticFallbackSource{},
	)

	depth, err := adapter.Depth(context.Background(), []string{"SPY"}, 5)
	if err != nil {
		t.Fatalf("Depth error = %v", err)
	}
	spy := depth["SPY"]
	if spy.Source != "ibkr" || spy.Basis != string(marketdata.BasisDelayed) {
		t.Fatalf("SPY depth source/basis = %q/%q, want ibkr/%s", spy.Source, spy.Basis, marketdata.BasisDelayed)
	}
	if len(spy.Levels) != 4 {
		t.Fatalf("SPY depth levels = %d, want 4", len(spy.Levels))
	}
	if spy.Levels[0].Side != "bid" || spy.Levels[0].Size != 1000 || spy.Levels[2].Side != "ask" || spy.Levels[2].Price != 500.1 {
		t.Fatalf("SPY depth levels not preserved: %#v", spy.Levels)
	}
}

func TestServiceWarnsWhenBrokerPreferredQuotesUseFallback(t *testing.T) {
	now := fixedShockNow()
	adapter := NewBrokerQuoteAdapter(
		func(context.Context) (broker.Broker, string, error) {
			return nil, "", errors.New("ibkr offline")
		},
		staticFallbackSource{quotes: map[string]ShockQuote{
			"VIX": {ID: "VIX", Label: "VIX", Price: 28, ChangePct: 25, Source: "yfinance", Basis: "delayed", AsOf: now},
			"SPY": {ID: "SPY", Label: "SPY", Price: 500, ChangePct: -2.5, Bid: 499.9, Ask: 500.1, Source: "yfinance", Basis: "delayed", AsOf: now},
			"QQQ": {ID: "QQQ", Label: "QQQ", Price: 430, ChangePct: -3, Bid: 429.9, Ask: 430.1, Source: "yfinance", Basis: "delayed", AsOf: now},
			"HYG": {ID: "HYG", Label: "HYG", Price: 75, ChangePct: -1.2, Bid: 74.8, Ask: 75.2, Source: "yfinance", Basis: "delayed", AsOf: now},
			"TLT": {ID: "TLT", Label: "TLT", Price: 94, ChangePct: 1.5, Bid: 93.9, Ask: 94.1, Source: "yfinance", Basis: "delayed", AsOf: now},
		}},
	)
	svc := NewService(adapter)
	svc.Now = fixedShockNow

	dto, err := svc.Regime(context.Background())
	if err != nil {
		t.Fatalf("Regime error = %v", err)
	}
	if dto.Source != "yfinance" {
		t.Fatalf("Regime source = %q, want yfinance fallback", dto.Source)
	}
	if !warningsContain(dto.Warnings, "broker quotes") || !warningsContain(dto.Warnings, "ibkr offline") {
		t.Fatalf("warnings = %#v, want broker fallback warning", dto.Warnings)
	}
}

func TestServiceWarnsWhenBrokerConnectorUsesYFinanceFallback(t *testing.T) {
	now := fixedShockNow()
	adapter := NewBrokerQuoteAdapter(
		func(context.Context) (broker.Broker, string, error) {
			return testBroker{quotes: map[string]*model.StockQuote{
				"SPY": {Symbol: "SPY", Last: 501, Bid: 500.9, Ask: 501.1, Change: -14, ChangePct: -2.72, Timestamp: now},
			}}, "Yahoo Finance", nil
		},
		staticFallbackSource{quotes: map[string]ShockQuote{
			"VIX": {ID: "VIX", Label: "VIX", Price: 28, ChangePct: 25, Source: "yfinance", Basis: "delayed", AsOf: now},
			"SPY": {ID: "SPY", Label: "SPY", Price: 500, ChangePct: -2.5, Bid: 499.9, Ask: 500.1, Source: "yfinance", Basis: "delayed", AsOf: now},
			"QQQ": {ID: "QQQ", Label: "QQQ", Price: 430, ChangePct: -3, Bid: 429.9, Ask: 430.1, Source: "yfinance", Basis: "delayed", AsOf: now},
			"HYG": {ID: "HYG", Label: "HYG", Price: 75, ChangePct: -1.2, Bid: 74.8, Ask: 75.2, Source: "yfinance", Basis: "delayed", AsOf: now},
			"TLT": {ID: "TLT", Label: "TLT", Price: 94, ChangePct: 1.5, Bid: 93.9, Ask: 94.1, Source: "yfinance", Basis: "delayed", AsOf: now},
		}},
	)
	svc := NewService(adapter)
	svc.Now = fixedShockNow

	dto, err := svc.Regime(context.Background())
	if err != nil {
		t.Fatalf("Regime error = %v", err)
	}
	if !warningsContain(dto.Warnings, "broker quotes degraded") || !warningsContain(dto.Warnings, "yfinance") {
		t.Fatalf("warnings = %#v, want source fallback warning", dto.Warnings)
	}
}

func TestServiceCapsBrokerOverlayWhenFallbackQuotesExist(t *testing.T) {
	now := fixedShockNow()
	adapter := NewBrokerQuoteAdapter(
		func(ctx context.Context) (broker.Broker, string, error) {
			<-ctx.Done()
			return nil, "", ctx.Err()
		},
		staticFallbackSource{quotes: map[string]ShockQuote{
			"VIX": {ID: "VIX", Label: "VIX", Price: 28, ChangePct: 25, Source: "yfinance", Basis: "delayed", AsOf: now},
			"SPY": {ID: "SPY", Label: "SPY", Price: 500, ChangePct: -2.5, Bid: 499.9, Ask: 500.1, Source: "yfinance", Basis: "delayed", AsOf: now},
		}},
	)
	adapter.overlayTimeout = 10 * time.Millisecond
	svc := NewService(adapter)
	svc.Now = fixedShockNow

	start := time.Now()
	dto, err := svc.Regime(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Regime error = %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("Regime elapsed = %s, want broker overlay capped", elapsed)
	}
	if !warningsContain(dto.Warnings, "broker quotes degraded") || !warningsContain(dto.Warnings, "deadline") {
		t.Fatalf("warnings = %#v, want broker timeout warning", dto.Warnings)
	}
}

func TestBrokerQuoteAdapterCapsFallbackQuotesWhenBrokerOverlayCanRecover(t *testing.T) {
	now := fixedShockNow()
	adapter := NewBrokerQuoteAdapter(
		func(context.Context) (broker.Broker, string, error) {
			return testBroker{quotes: map[string]*model.StockQuote{
				"SPY": {Symbol: "SPY", Last: 501, Bid: 500.9, Ask: 501.1, Change: -14, ChangePct: -2.72, Timestamp: now},
			}}, "IBKR", nil
		},
		blockingFallbackSource{},
	)
	adapter.overlayTimeout = 10 * time.Millisecond
	adapter.fallbackTimeout = 10 * time.Millisecond

	start := time.Now()
	quotes, err := adapter.Quotes(context.Background(), []string{"SPY"})
	elapsed := time.Since(start)
	if elapsed > 200*time.Millisecond {
		t.Fatalf("Quotes elapsed = %s, want fallback quotes capped", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "fallback quotes degraded") || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("Quotes error = %v, want fallback timeout warning", err)
	}
	if quotes["SPY"].Source != "ibkr" || quotes["SPY"].Bid != 500.9 || quotes["SPY"].Ask != 501.1 {
		t.Fatalf("SPY not recovered from broker overlay: %#v", quotes["SPY"])
	}
}

type fakeShockSource struct {
	quoteErr      error
	barErr        error
	depthErr      error
	optionErr     error
	optionMetrics map[string]OptionStress
}

func (f *fakeShockSource) Quotes(context.Context, []string) (map[string]ShockQuote, error) {
	if f.quoteErr != nil {
		return nil, f.quoteErr
	}
	now := fixedShockNow()
	return map[string]ShockQuote{
		"VIX":   {ID: "VIX", Label: "VIX", Price: 28, ChangePct: 25, Source: "ibkr", Basis: "realtime", AsOf: now},
		"SPY":   {ID: "SPY", Label: "SPY", Price: 500, ChangePct: -2.5, Bid: 499.9, Ask: 500.1, BidSize: 1000, AskSize: 900, Source: "ibkr", Basis: "realtime", AsOf: now},
		"QQQ":   {ID: "QQQ", Label: "QQQ", Price: 430, ChangePct: -3, Bid: 429.9, Ask: 430.1, Source: "ibkr", Basis: "realtime", AsOf: now},
		"IWM":   {ID: "IWM", Label: "IWM", Price: 205, ChangePct: -2, Bid: 204.9, Ask: 205.1, Source: "ibkr", Basis: "realtime", AsOf: now},
		"TLT":   {ID: "TLT", Label: "TLT", Price: 94, ChangePct: 1.5, Bid: 93.9, Ask: 94.1, Source: "ibkr", Basis: "realtime", AsOf: now},
		"HYG":   {ID: "HYG", Label: "HYG", Price: 75, ChangePct: -1.2, Bid: 74.8, Ask: 75.2, Source: "ibkr", Basis: "realtime", AsOf: now},
		"LQD":   {ID: "LQD", Label: "LQD", Price: 108, ChangePct: -0.8, Bid: 107.9, Ask: 108.1, Source: "ibkr", Basis: "realtime", AsOf: now},
		"US10Y": {ID: "US10Y", Label: "US 10Y", Price: 4.4, ChangePct: 0.9, Source: "ibkr", Basis: "realtime", AsOf: now},
		"UUP":   {ID: "UUP", Label: "UUP", Price: 31, ChangePct: 0.8, Source: "ibkr", Basis: "realtime", AsOf: now},
		"USO":   {ID: "USO", Label: "USO", Price: 80, ChangePct: -3.0, Source: "ibkr", Basis: "realtime", AsOf: now},
		"GLD":   {ID: "GLD", Label: "GLD", Price: 220, ChangePct: 1.0, Source: "ibkr", Basis: "realtime", AsOf: now},
	}, nil
}

func (f *fakeShockSource) Bars(context.Context, []string, string, time.Duration) (map[string][]model.OHLCV, error) {
	if f.barErr != nil {
		return nil, f.barErr
	}
	return map[string][]model.OHLCV{}, nil
}

func (f *fakeShockSource) Depth(context.Context, []string, int) (map[string]DepthSnapshot, error) {
	if f.depthErr != nil {
		return nil, f.depthErr
	}
	now := fixedShockNow()
	return map[string]DepthSnapshot{
		"SPY": {ID: "SPY", Source: "ibkr", Basis: "realtime", AsOf: now, Levels: []DepthLevel{
			{Side: "bid", Price: 499.9, Size: 1000},
			{Side: "ask", Price: 500.1, Size: 900},
		}},
	}, nil
}

func (f *fakeShockSource) OptionMetrics(context.Context, []string) (map[string]OptionStress, error) {
	if f.optionErr != nil {
		return nil, f.optionErr
	}
	if f.optionMetrics != nil {
		return f.optionMetrics, nil
	}
	return map[string]OptionStress{}, nil
}

type countingShockSource struct {
	inner *fakeShockSource

	mu          sync.Mutex
	quoteCounts map[string]int
	depthCalls  int
}

func (c *countingShockSource) Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error) {
	quotes, err := c.inner.Quotes(ctx, ids)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.quoteCounts == nil {
		c.quoteCounts = map[string]int{}
	}
	for _, id := range uniqueStrings(ids) {
		c.quoteCounts[id]++
	}
	return quotes, err
}

func (c *countingShockSource) Bars(ctx context.Context, ids []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error) {
	return c.inner.Bars(ctx, ids, interval, lookback)
}

func (c *countingShockSource) Depth(ctx context.Context, ids []string, levels int) (map[string]DepthSnapshot, error) {
	depth, err := c.inner.Depth(ctx, ids, levels)
	c.mu.Lock()
	c.depthCalls++
	c.mu.Unlock()
	return depth, err
}

func (c *countingShockSource) OptionMetrics(ctx context.Context, underlyings []string) (map[string]OptionStress, error) {
	return c.inner.OptionMetrics(ctx, underlyings)
}

func (c *countingShockSource) quoteFetches(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.quoteCounts[id]
}

func warningsContain(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}

func fingerprintEvidenceContains(rows []FingerprintRow, needle string) bool {
	for _, row := range rows {
		for _, evidence := range row.Evidence {
			if strings.Contains(evidence, needle) {
				return true
			}
		}
	}
	return false
}

type staticFallbackSource struct {
	quotes map[string]ShockQuote
}

func (s staticFallbackSource) Quotes(context.Context, []string) (map[string]ShockQuote, error) {
	return s.quotes, nil
}

func (staticFallbackSource) Bars(context.Context, []string, string, time.Duration) (map[string][]model.OHLCV, error) {
	return map[string][]model.OHLCV{}, nil
}

func (staticFallbackSource) Depth(context.Context, []string, int) (map[string]DepthSnapshot, error) {
	return map[string]DepthSnapshot{}, nil
}

func (staticFallbackSource) OptionMetrics(context.Context, []string) (map[string]OptionStress, error) {
	return map[string]OptionStress{}, nil
}

type blockingFallbackSource struct{}

func (blockingFallbackSource) Quotes(ctx context.Context, _ []string) (map[string]ShockQuote, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingFallbackSource) Bars(context.Context, []string, string, time.Duration) (map[string][]model.OHLCV, error) {
	return map[string][]model.OHLCV{}, nil
}

func (blockingFallbackSource) Depth(context.Context, []string, int) (map[string]DepthSnapshot, error) {
	return map[string]DepthSnapshot{}, nil
}

func (blockingFallbackSource) OptionMetrics(context.Context, []string) (map[string]OptionStress, error) {
	return map[string]OptionStress{}, nil
}

type testBroker struct {
	quotes map[string]*model.StockQuote
	depth  map[string]*model.MarketDepth
	chains map[string]*model.OptionChain
}

func (b testBroker) Connect(context.Context) error { return nil }

func (b testBroker) Disconnect() error { return nil }

func (b testBroker) IsConnected() bool { return true }

func (b testBroker) GetQuote(_ context.Context, symbol string) (*model.StockQuote, error) {
	if q, ok := b.quotes[symbol]; ok {
		return q, nil
	}
	return nil, errors.New("missing quote")
}

func (b testBroker) GetHistoricalBars(context.Context, string, string, string, string) ([]model.OHLCV, error) {
	return nil, nil
}

func (b testBroker) GetOptionChain(context.Context, string, string) (*model.OptionChain, error) {
	return nil, nil
}

func (b testBroker) GetOptionChainWithOI(_ context.Context, underlying string, _ string) (*model.OptionChain, error) {
	if ch, ok := b.chains[underlying]; ok {
		return ch, nil
	}
	return nil, errors.New("missing option chain")
}

func (b testBroker) GetMarketDepth(_ context.Context, symbol string, _ int) (*model.MarketDepth, error) {
	if d, ok := b.depth[symbol]; ok {
		return d, nil
	}
	return nil, errors.New("missing depth")
}
