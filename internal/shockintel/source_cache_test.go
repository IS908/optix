package shockintel

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCanceledMissPreservesCachedMarketData(t *testing.T) {
	src := &fakeShockSource{optionMetrics: map[string]OptionStress{"SPY": {OpenInt: 10}}}
	c := newTTLMarketCache(src, time.Minute)
	ctx := context.Background()
	c.Quotes(ctx, []string{"VIX"})
	c.Depth(ctx, []string{"SPY"}, 5)
	c.OptionMetrics(ctx, []string{"SPY"})
	src.quoteErr, src.depthErr, src.optionErr = context.Canceled, context.Canceled, context.Canceled
	quotes, _ := c.Quotes(ctx, []string{"VIX", "NEW"})
	depth, _ := c.Depth(ctx, []string{"SPY", "NEW"}, 5)
	options, _ := c.OptionMetrics(ctx, []string{"SPY", "NEW"})
	if quotes["VIX"].Price != 28 || len(depth["SPY"].Levels) == 0 || options["SPY"].OpenInt != 10 {
		t.Fatalf("cached data discarded: %v %v %v", quotes, depth, options)
	}
}

type firstCanceledSource struct {
	fakeShockSource
	mu      sync.Mutex
	first   bool
	started chan struct{}
}

func (s *firstCanceledSource) Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error) {
	s.mu.Lock()
	first := !s.first
	s.first = true
	s.mu.Unlock()
	if first {
		close(s.started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.fakeShockSource.Quotes(ctx, ids)
}

func TestLiveWaiterRecoversFromCanceledProbe(t *testing.T) {
	src := &firstCanceledSource{started: make(chan struct{})}
	c := newTTLMarketCache(src, time.Minute)
	probe, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); c.Quotes(probe, []string{"VIX"}) }()
	<-src.started
	quotes, err := c.Quotes(context.Background(), []string{"VIX"})
	<-done
	if err != nil || quotes["VIX"].Price != 28 {
		t.Fatalf("live waiter inherited probe cancellation: %v %v", quotes, err)
	}
}

type canceledQuoteSource struct {
	*fakeShockSource
	cancel context.CancelFunc
}

func (s *canceledQuoteSource) Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error) {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
		return nil, errors.New("fetcher killed")
	}
	return s.fakeShockSource.Quotes(ctx, ids)
}

func TestTTLCachePreservesCancellationWhenSourceLosesErrorType(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := newTTLMarketCache(&canceledQuoteSource{fakeShockSource: &fakeShockSource{}, cancel: cancel}, time.Minute)
	if _, err := cache.Quotes(ctx, []string{"VIX"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not preserved: %v", err)
	}
	quotes, err := cache.Quotes(context.Background(), []string{"VIX"})
	if err != nil || quotes["VIX"].Price != 28 {
		t.Fatalf("healthy request could not recover: %v, %v", quotes, err)
	}
}

func TestTTLCacheRetriesAfterRequestTimeout(t *testing.T) {
	src := &fakeShockSource{quoteErr: context.DeadlineExceeded, depthErr: context.Canceled, optionErr: context.DeadlineExceeded}
	cache := newTTLMarketCache(src, time.Minute)
	ctx := context.Background()
	if _, err := cache.Quotes(ctx, []string{"VIX"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := cache.Depth(ctx, []string{"SPY"}, 5); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := cache.OptionMetrics(ctx, []string{"SPY"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	src.quoteErr, src.depthErr, src.optionErr = nil, nil, nil
	src.optionMetrics = map[string]OptionStress{"SPY": {}}
	quotes, err := cache.Quotes(ctx, []string{"VIX"})
	if err != nil || quotes["VIX"].Price != 28 {
		t.Errorf("quotes poisoned by previous deadline: %v, %v", quotes, err)
	}
	depth, err := cache.Depth(ctx, []string{"SPY"}, 5)
	if err != nil || len(depth["SPY"].Levels) == 0 {
		t.Errorf("depth poisoned by previous cancellation: %v, %v", depth, err)
	}
	options, err := cache.OptionMetrics(ctx, []string{"SPY"})
	if err != nil || len(options) != 1 {
		t.Errorf("options poisoned by previous deadline: %v, %v", options, err)
	}
}
