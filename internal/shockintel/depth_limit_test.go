package shockintel

import (
	"context"
	"fmt"
	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/pkg/model"
	"sync/atomic"
	"testing"
	"time"
)

type limitedDepthBroker struct {
	testBroker
	active atomic.Int32
}

func (b *limitedDepthBroker) GetMarketDepth(ctx context.Context, symbol string, levels int) (*model.MarketDepth, error) {
	n := b.active.Add(1)
	defer b.active.Add(-1)
	if n > 3 {
		return nil, fmt.Errorf("309: maximum 3 depth requests")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Millisecond):
	}
	return &model.MarketDepth{Symbol: symbol, Levels: []model.MarketDepthLevel{{Side: "bid", Price: 100, Size: 5}, {Side: "ask", Price: 101, Size: 5}}}, nil
}
func TestDepthRespectsGatewayThreeSubscriptionLimit(t *testing.T) {
	b := &limitedDepthBroker{}
	a := NewBrokerQuoteAdapter(func(context.Context) (broker.Broker, string, error) { return b, "ibkr", nil }, nil)
	rows, err := a.Depth(context.Background(), []string{"SPY", "QQQ", "IWM", "TLT", "HYG", "LQD"}, 5)
	if err != nil || len(rows) != 6 {
		t.Fatalf("depth quota lost rows: %d %v", len(rows), err)
	}
}

type retryDepthBroker struct {
	testBroker
	calls int
}

func (b *retryDepthBroker) GetMarketDepth(context.Context, string, int) (*model.MarketDepth, error) {
	b.calls++
	if b.calls == 1 {
		return nil, broker.ErrMarketDepthLimit
	}
	return &model.MarketDepth{Symbol: "SPY"}, nil
}
func TestDepthRetriesTemporaryQuotaAndHonorsCancellation(t *testing.T) {
	b := &retryDepthBroker{}
	got, err := fetchDepthWithQuotaRetry(context.Background(), b, "SPY", 5)
	if err != nil || got == nil || b.calls != 2 {
		t.Fatalf("quota recovery failed: %+v %v %d", got, err, b.calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b = &retryDepthBroker{}
	_, err = fetchDepthWithQuotaRetry(ctx, b, "SPY", 5)
	if err != context.Canceled || b.calls != 0 {
		t.Fatalf("canceled request retried: %v %d", err, b.calls)
	}
}
