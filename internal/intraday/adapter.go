package intraday

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
	"github.com/IS908/optix/internal/intelshared"
	"github.com/IS908/optix/pkg/model"
)

type sourceNamer interface {
	SourceName() string
}

type BrokerConnector func(ctx context.Context) (broker.Broker, string, error)

type BrokerSource struct {
	connect BrokerConnector
	source  string
	basis   string

	// Now is an injectable clock for lookback→startDate derivation (tests
	// only; defaults to time.Now).
	Now func() time.Time
}

var intradayBrokerClientID int64 = 7200

func NewBrokerSource(b broker.Broker, source, basis string) *BrokerSource {
	return NewBrokerSourceWithConnector(func(context.Context) (broker.Broker, string, error) {
		if b == nil {
			return nil, "", fmt.Errorf("broker is nil")
		}
		actualSource := source
		if namer, ok := b.(sourceNamer); ok {
			actualSource = namer.SourceName()
		}
		return b, normalizeSourceName(actualSource), nil
	}, source, basis)
}

func NewBrokerSourceWithConnector(connect BrokerConnector, source, basis string) *BrokerSource {
	if source == "" {
		source = "ibkr-preferred"
	}
	if basis == "" {
		basis = "realtime"
	}
	return &BrokerSource{
		connect: connect,
		source:  normalizeSourceName(source),
		basis:   basis,
	}
}

func NewIBKRPreferredSource(host string, port int, pythonBin string) *BrokerSource {
	return NewBrokerSourceWithConnector(func(ctx context.Context) (broker.Broker, string, error) {
		clientID := atomic.AddInt64(&intradayBrokerClientID, 1)
		b := factory.NewWithFallback(ibkr.Config{Host: host, Port: port, ClientID: clientID}, pythonBin)
		if err := b.Connect(ctx); err != nil {
			return nil, "", err
		}
		return b, normalizeSourceName(b.SourceName()), nil
	}, "ibkr-preferred", "realtime")
}

func (s *BrokerSource) SourceName() string { return s.source }

func (s *BrokerSource) Basis() string { return s.basis }

func (s *BrokerSource) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *BrokerSource) Quotes(ctx context.Context, symbols []string) (map[string]Quote, error) {
	b, source, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = b.Disconnect() }()
	source = normalizeSourceName(source)
	return quotesFromBroker(ctx, b, source, symbols), nil
}

func (s *BrokerSource) Bars(ctx context.Context, symbols []string, timeframe string, lookback time.Duration) (map[string][]model.OHLCV, error) {
	b, source, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = b.Disconnect() }()
	source = normalizeSourceName(source)
	startDate := lookbackStartDate(s.now(), lookback)
	return barsFromBroker(ctx, b, source, symbols, timeframe, startDate), nil
}

func (s *BrokerSource) Snapshot(ctx context.Context, symbols []string, timeframe string, lookback time.Duration) (map[string]Quote, map[string][]model.OHLCV, error) {
	b, source, err := s.connect(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = b.Disconnect() }()
	source = normalizeSourceName(source)
	startDate := lookbackStartDate(s.now(), lookback)
	// Interleave independent quote/bar jobs so a slow quote cannot consume
	// the whole snapshot deadline before any historical request starts.
	quotes := map[string]Quote{}
	bars := map[string][]model.OHLCV{}
	jobs := make(chan func(), len(symbols)*2)
	var mu sync.Mutex
	for _, symbol := range symbols {
		jobs <- func() {
			got := quotesFromBroker(ctx, b, source, []string{symbol})
			mu.Lock()
			defer mu.Unlock()
			for id, q := range got {
				quotes[id] = q
			}
		}
		jobs <- func() {
			got := barsFromBroker(ctx, b, source, []string{symbol}, timeframe, startDate)
			mu.Lock()
			defer mu.Unlock()
			for id, rows := range got {
				bars[id] = rows
			}
		}
	}
	close(jobs)
	var wg sync.WaitGroup
	for range min(8, len(symbols)*2) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				job()
			}
		}()
	}
	wg.Wait()
	return quotes, bars, nil
}

// lookbackStartDate derives a broker-compatible startDate (YYYYMMDD, NY
// calendar date) from a lookback duration. Passing a real startDate lets the
// IBKR and yfinance clients size their historical-data request instead of
// defaulting to a multi-month/year window: IBKR rejects that for 5-min bars
// (pacing violation, empty result) and yfinance's 5m endpoint caps at ~60
// days, also returning empty past that — the root cause of #191 finding 1
// (movers/heatmap silently empty on both real data paths).
func lookbackStartDate(now time.Time, lookback time.Duration) string {
	if lookback <= 0 {
		return ""
	}
	return now.Add(-lookback).In(intelshared.NY()).Format("20060102")
}

// quotesFromBroker fetches one quote per symbol sequentially. It checks
// ctx.Err() at the top of each iteration so a canceled/expired context
// (e.g. Service.LoadTimeout firing mid-loop) stops issuing new broker calls
// promptly instead of grinding through the remaining symbols (#191 finding
// 2). Symbols it didn't get to are simply absent from the result — the
// caller (service.go) turns a shorter-than-requested result into an
// explicit "N/M symbols unavailable" warning rather than pretending the
// result set is complete.
func quotesFromBroker(ctx context.Context, b broker.Broker, source string, symbols []string) map[string]Quote {
	basis := basisForSource(source)
	out := map[string]Quote{}
	for _, symbol := range symbols {
		if ctx.Err() != nil {
			break
		}
		q, err := b.GetQuote(ctx, symbol)
		if err != nil || q == nil {
			continue
		}
		quoteSymbol := q.Symbol
		if quoteSymbol == "" {
			quoteSymbol = symbol
		}
		out[symbol] = Quote{
			Symbol: quoteSymbol,
			Source: source,
			Basis:  basis,
			Last:   q.Last,
			Bid:    q.Bid,
			Ask:    q.Ask,
			Close:  q.Close,
			Volume: q.Volume,
			AsOf:   q.Timestamp,
		}
	}
	return out
}

// barsFromBroker fetches historical bars one symbol at a time, aborting
// promptly on ctx cancellation for the same reason as quotesFromBroker
// (#191 finding 2).
func barsFromBroker(ctx context.Context, b broker.Broker, source string, symbols []string, timeframe, startDate string) map[string][]model.OHLCV {
	out := map[string][]model.OHLCV{}
	for _, symbol := range symbols {
		if ctx.Err() != nil {
			break
		}
		bars, err := b.GetHistoricalBars(ctx, symbol, timeframe, startDate, "")
		if err != nil {
			continue
		}
		out[symbol] = normalizeBarVolume(bars, source)
	}
	return out
}

// normalizeBarVolume is the #191 finding-4 hook for keeping the DTO's Volume
// field in one consistent unit (shares) regardless of upstream source.
//
// The issue's audit asserted IBKR historical-bar volume is reported in round
// lots of 100 shares (the long-standing pre-Decimal TWS API convention) and
// asked for a x100 scale-up here. Live verification against IB Gateway
// during #191 (2026-08-08, AAPL/NVDA, IB Gateway 127.0.0.1:4001) disproved
// that premise for the ibapi client actually vendored in this repo
// (github.com/scmhub/ibapi, whose Bar.Volume is a Decimal backed by
// github.com/robaho/fixed): a single 5-minute AAPL bar came back reporting
// ~1.27M raw units, and summing a full RTH session's bars for NVDA totaled
// ~20M raw units — both exactly in the range of normal per-bar/per-day share
// volume. Applying x100 on top (as the audit prescribed) inflated those to
// ~127M-shares-in-5-minutes and a ~2B-share NVDA session — implausible by
// 1-2 orders of magnitude, and strictly worse than the pre-#191 behavior
// (unscaled) it would have replaced. IB's Decimal-typed size/volume fields
// (introduced for fractional-share support, TWS API 10.x+) already report
// actual share counts, not lots; the lot convention only applied to the
// legacy int-typed fields this library has moved past.
//
// So: no scaling. This is intentionally a documented no-op (kept as a named
// function, not deleted outright) so the source-threading plumbing added for
// this finding stays in place and a future genuinely lot-denominated source
// has an obvious place to normalize.
func normalizeBarVolume(bars []model.OHLCV, _ string) []model.OHLCV {
	return bars
}

func normalizeSourceName(value string) string {
	low := strings.ToLower(strings.TrimSpace(value))
	switch {
	case low == "ibkr-preferred":
		return "ibkr-preferred"
	case strings.Contains(low, "ibkr") || strings.Contains(low, "interactive brokers"):
		return "ibkr"
	case strings.Contains(low, "yahoo") || strings.Contains(low, "yfinance"):
		return "yfinance"
	case low == "":
		return "unknown"
	default:
		return low
	}
}

func basisForSource(source string) string {
	if source == "ibkr" {
		return "realtime"
	}
	return "delayed"
}
