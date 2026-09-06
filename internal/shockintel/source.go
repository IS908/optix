package shockintel

import (
	"context"
	"errors"
	"fmt"
	"math"
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

type MarketSource interface {
	Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error)
	Bars(ctx context.Context, ids []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error)
	Depth(ctx context.Context, ids []string, levels int) (map[string]DepthSnapshot, error)
	OptionMetrics(ctx context.Context, underlyings []string) (map[string]OptionStress, error)
}

type BrokerConnector func(ctx context.Context) (broker.Broker, string, error)

type BrokerQuoteAdapter struct {
	connect         BrokerConnector
	fallback        MarketSource
	overlayTimeout  time.Duration
	fallbackTimeout time.Duration
}

type YFinanceAdapter struct {
	src       *marketdata.YFinanceSource
	pythonBin string
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

func NewBrokerQuoteAdapter(connect BrokerConnector, fallback MarketSource) *BrokerQuoteAdapter {
	return &BrokerQuoteAdapter{connect: connect, fallback: fallback, overlayTimeout: 1500 * time.Millisecond, fallbackTimeout: 8 * time.Second}
}

func NewYFinanceAdapter(pythonBin string) *YFinanceAdapter {
	return &YFinanceAdapter{src: marketdata.NewYFinanceSource(pythonBin), pythonBin: pythonBin}
}

func (a *BrokerQuoteAdapter) Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error) {
	out := map[string]ShockQuote{}
	var fallbackErr error
	if a.fallback != nil {
		fallbackCtx := ctx
		var cancelFallback context.CancelFunc
		if a.fallbackTimeout > 0 {
			fallbackCtx, cancelFallback = context.WithTimeout(ctx, a.fallbackTimeout)
		}
		fallbackQuotes, err := a.fallback.Quotes(fallbackCtx, ids)
		if cancelFallback != nil {
			cancelFallback()
		}
		if err != nil {
			fallbackErr = fmt.Errorf("fallback quotes degraded: %w", err)
		}
		for id, q := range fallbackQuotes {
			out[id] = q
		}
	}

	brokerIDs := brokerQuoteIDs(ids)
	if len(brokerIDs) == 0 || a.connect == nil {
		if len(out) > 0 {
			return out, fallbackErr
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

func (a *BrokerQuoteAdapter) OptionMetrics(ctx context.Context, underlyings []string) (map[string]OptionStress, error) {
	out := map[string]OptionStress{}
	ids := make([]string, 0, len(underlyings))
	for _, id := range underlyings {
		ids = append(ids, strings.ToUpper(strings.TrimSpace(id)))
	}
	ids = uniqueStrings(ids)
	if len(ids) == 0 {
		return out, nil
	}
	if a.connect == nil {
		return out, fmt.Errorf("broker option stress unavailable: no broker connector")
	}

	optionCtx := ctx
	var cancelOption context.CancelFunc
	if a.overlayTimeout > 0 {
		optionCtx, cancelOption = context.WithTimeout(ctx, 6*time.Second)
		defer cancelOption()
	}

	b, source, err := a.connect(optionCtx)
	if err != nil {
		return out, fmt.Errorf("broker option stress degraded: %w", err)
	}
	source = normalizeSourceName(source)
	defer b.Disconnect()

	var warnParts []string
	if source != "ibkr" {
		warnParts = append(warnParts, fmt.Sprintf("broker option stress degraded: using %s fallback for option chain", source))
	}
	// At most two full chains at a time (each IBKR chain has its own
	// bounded OI pool). Reserve a share of the deadline for later waves.
	perSymbol := 6 * time.Second
	if deadline, ok := optionCtx.Deadline(); ok {
		perSymbol = time.Until(deadline)
	}
	perSymbol /= time.Duration((len(ids) + 1) / 2)
	type result struct {
		row OptionStress
		ok  bool
		err error
	}
	results := make([]result, len(ids))
	jobs := make(chan int, len(ids))
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	for range min(2, len(ids)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				itemCtx, cancel := context.WithTimeout(optionCtx, perSymbol)
				chain, err := optionStressChain(itemCtx, b, ids[i])
				err = errors.Join(err, itemCtx.Err())
				cancel()
				row, ok := summarizeOptionStress(ids[i], chain, source, time.Now().UTC())
				results[i] = result{row: row, ok: ok, err: err}
			}
		}()
	}
	wg.Wait()
	for i, r := range results {
		if r.err != nil {
			warnParts = append(warnParts, fmt.Sprintf("%s: %v", ids[i], r.err))
		}
		if r.ok {
			out[ids[i]] = r.row
		} else if r.err == nil {
			warnParts = append(warnParts, fmt.Sprintf("%s: option chain missing IV/OI/volume", ids[i]))
		}
	}
	if len(warnParts) > 0 {
		err := fmt.Errorf("option stress partial: %s", strings.Join(warnParts, "; "))
		if len(out) == 0 {
			return out, err
		}
		return out, err
	}
	return out, nil
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

func (a *YFinanceAdapter) OptionMetrics(ctx context.Context, underlyings []string) (map[string]OptionStress, error) {
	out := map[string]OptionStress{}
	var warnParts []string
	for _, underlying := range uniqueStrings(underlyings) {
		chain, err := marketdata.OptionChainWithOI(ctx, a.pythonBin, underlying, "")
		if err != nil {
			warnParts = append(warnParts, fmt.Sprintf("%s: %v", underlying, err))
			continue
		}
		row, ok := summarizeOptionStress(underlying, chain, "yfinance", time.Now().UTC())
		if !ok {
			warnParts = append(warnParts, fmt.Sprintf("%s: option chain missing IV/OI/volume", underlying))
			continue
		}
		out[underlying] = row
	}
	if len(warnParts) > 0 {
		err := fmt.Errorf("option stress partial: %s", strings.Join(warnParts, "; "))
		if len(out) == 0 {
			return out, err
		}
		return out, err
	}
	return out, nil
}

func optionStressChain(ctx context.Context, b broker.Broker, underlying string) (*model.OptionChain, error) {
	if f, ok := b.(broker.OIFetcher); ok {
		return f.GetOptionChainWithOI(ctx, underlying, "")
	}
	return b.GetOptionChain(ctx, underlying, "")
}

func summarizeOptionStress(underlying string, chain *model.OptionChain, source string, asOf time.Time) (OptionStress, bool) {
	if chain == nil || len(chain.Expirations) == 0 {
		return OptionStress{}, false
	}
	exp := chain.Expirations[0]
	if len(exp.Calls) == 0 && len(exp.Puts) == 0 {
		return OptionStress{}, false
	}
	spot := chain.UnderlyingPrice
	if spot <= 0 {
		spot = inferSpotFromChain(exp)
	}
	call, hasCall := nearestOption(exp.Calls, spot)
	put, hasPut := nearestOption(exp.Puts, spot)

	var volume int64
	var openInt int64
	for _, q := range exp.Calls {
		volume += q.Volume
		openInt += int64(q.OpenInterest)
	}
	for _, q := range exp.Puts {
		volume += q.Volume
		openInt += int64(q.OpenInterest)
	}

	callIV, putIV := 0.0, 0.0
	if hasCall {
		callIV = call.ImpliedVolatility
	}
	if hasPut {
		putIV = put.ImpliedVolatility
	}
	atmIV := averagePositive(callIV, putIV)
	ivSkew := 0.0
	if callIV > 0 && putIV > 0 {
		ivSkew = putIV - callIV
	}
	if atmIV <= 0 && volume == 0 && openInt == 0 {
		return OptionStress{}, false
	}

	note := fmt.Sprintf("exp=%s", exp.Expiration)
	if atmIV > 0 {
		note = fmt.Sprintf("%s atm_iv=%.2f", note, atmIV)
	}
	if ivSkew != 0 {
		note = fmt.Sprintf("%s put_call_iv_skew=%.2f", note, ivSkew)
	}
	return OptionStress{
		Underlying: underlying,
		Source:     source,
		Basis:      basisForSource(source),
		AsOf:       asOf.UTC(),
		IVSkew:     ivSkew,
		Volume:     volume,
		OpenInt:    openInt,
		Note:       note,
	}, true
}

func nearestOption(options []model.OptionQuote, spot float64) (model.OptionQuote, bool) {
	if len(options) == 0 {
		return model.OptionQuote{}, false
	}
	best := options[0]
	bestDist := math.Abs(best.Strike - spot)
	for _, option := range options[1:] {
		dist := math.Abs(option.Strike - spot)
		if dist < bestDist {
			best = option
			bestDist = dist
		}
	}
	return best, true
}

func inferSpotFromChain(exp model.OptionChainExpiry) float64 {
	if len(exp.Calls) > 0 {
		return exp.Calls[len(exp.Calls)/2].Strike
	}
	if len(exp.Puts) > 0 {
		return exp.Puts[len(exp.Puts)/2].Strike
	}
	return 0
}

func averagePositive(values ...float64) float64 {
	total := 0.0
	count := 0
	for _, value := range values {
		if value > 0 {
			total += value
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func shockRefsForIDs(ids []string) []marketdata.AssetRef {
	refs := make([]marketdata.AssetRef, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		ref, ok := marketdata.LookupAssetRef(id)
		if !ok {
			continue
		}
		refs = append(refs, ref)
		seen[id] = struct{}{}
	}
	return refs
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

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		out = append(out, value)
		seen[value] = struct{}{}
	}
	return out
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
	switch source {
	case "yfinance", "ibkr":
		return string(marketdata.BasisDelayed)
	default:
		return string(marketdata.BasisDelayed)
	}
}
