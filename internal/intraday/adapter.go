package intraday

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/factory"
	"github.com/IS908/optix/internal/broker/ibkr"
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

func (s *BrokerSource) Quotes(ctx context.Context, symbols []string) (map[string]Quote, error) {
	b, source, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = b.Disconnect() }()
	source = normalizeSourceName(source)
	return quotesFromBroker(ctx, b, source, symbols), nil
}

func (s *BrokerSource) Bars(ctx context.Context, symbols []string, timeframe string, _ time.Duration) (map[string][]model.OHLCV, error) {
	b, _, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = b.Disconnect() }()
	return barsFromBroker(ctx, b, symbols, timeframe), nil
}

func (s *BrokerSource) Snapshot(ctx context.Context, symbols []string, timeframe string, _ time.Duration) (map[string]Quote, map[string][]model.OHLCV, error) {
	b, source, err := s.connect(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = b.Disconnect() }()
	source = normalizeSourceName(source)
	return quotesFromBroker(ctx, b, source, symbols), barsFromBroker(ctx, b, symbols, timeframe), nil
}

func quotesFromBroker(ctx context.Context, b broker.Broker, source string, symbols []string) map[string]Quote {
	basis := basisForSource(source)
	out := map[string]Quote{}
	for _, symbol := range symbols {
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

func barsFromBroker(ctx context.Context, b broker.Broker, symbols []string, timeframe string) map[string][]model.OHLCV {
	out := map[string][]model.OHLCV{}
	for _, symbol := range symbols {
		bars, err := b.GetHistoricalBars(ctx, symbol, timeframe, "", "")
		if err != nil {
			continue
		}
		out[symbol] = bars
	}
	return out
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
