package shockintel

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/pkg/model"
)

type delayedQuoteSource struct{ fakeShockSource }

func (s delayedQuoteSource) Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error) {
	select {
	case <-time.After(30 * time.Millisecond):
		return s.fakeShockSource.Quotes(ctx, ids)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestBaseQuotesCanOutlastOptionalOverlayBudget(t *testing.T) {
	a := NewBrokerQuoteAdapter(nil, &delayedQuoteSource{})
	a.overlayTimeout = time.Millisecond
	quotes, err := a.Quotes(context.Background(), []string{"VIX"})
	if err != nil || quotes["VIX"].Price != 28 {
		t.Fatalf("base quotes cut off by overlay budget: %v %v", quotes, err)
	}
}

type slowOptionBroker struct{ testBroker }

func (b slowOptionBroker) GetOptionChainWithOI(ctx context.Context, symbol, expiry string) (*model.OptionChain, error) {
	return b.GetOptionChain(ctx, symbol, expiry)
}

func (b slowOptionBroker) GetOptionChain(ctx context.Context, symbol, expiry string) (*model.OptionChain, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.HasPrefix(symbol, "SLOW") {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &model.OptionChain{UnderlyingPrice: 100, Expirations: []model.OptionChainExpiry{{Calls: []model.OptionQuote{{Strike: 100, ImpliedVolatility: 0.3, OpenInterest: 10}}}}}, nil
}

func TestOptionMetricsSlowUnderlyingDoesNotStarveFollowingSymbol(t *testing.T) {
	a := NewBrokerQuoteAdapter(func(context.Context) (broker.Broker, string, error) { return slowOptionBroker{}, "ibkr", nil }, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	rows, err := a.OptionMetrics(ctx, []string{"SLOW", "FAST"})
	if err == nil {
		t.Fatal("expected slow underlying warning")
	}
	if rows["FAST"].OpenInt != 10 {
		t.Fatalf("healthy underlying starved: %v, %v", rows, err)
	}
}

func TestOptionMetricsReservesTimeForLaterWave(t *testing.T) {
	a := NewBrokerQuoteAdapter(func(context.Context) (broker.Broker, string, error) {
		return slowOptionBroker{}, "ibkr", nil
	}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	rows, err := a.OptionMetrics(ctx, []string{"SLOW1", "SLOW2", "FAST"})
	if err == nil || rows["FAST"].OpenInt != 10 {
		t.Fatalf("later wave starved: %v %v", rows, err)
	}
}

func TestOptionMetricsEmptySymbolsReturnEmpty(t *testing.T) {
	a := NewBrokerQuoteAdapter(func(context.Context) (broker.Broker, string, error) {
		return slowOptionBroker{}, "ibkr", nil
	}, nil)
	rows, err := a.OptionMetrics(context.Background(), []string{"", " "})
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty symbols: %v %v", rows, err)
	}
}
