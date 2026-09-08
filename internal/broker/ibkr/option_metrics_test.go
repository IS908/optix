package ibkr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOptionMetricsPreserveTicksAtCancellation(t *testing.T) {
	po := &pendingOI{done: make(chan struct{})}
	po.setVolume(21)
	po.setIV(.3)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := waitOptionMetrics(ctx, po, make(chan error))
	if !errors.Is(err, context.Canceled) || got.volume != 21 || got.iv != .3 {
		t.Fatalf("partial ticks lost: %+v %v", got, err)
	}
}

func TestOptionMetricsPreserveTicksAtError(t *testing.T) {
	po := &pendingOI{done: make(chan struct{})}
	po.setVolume(21)
	errs := make(chan error, 1)
	errs <- errors.New("missing subscription")
	got, err := waitOptionMetrics(context.Background(), po, errs)
	if err == nil || got.volume != 21 {
		t.Fatalf("partial ticks lost: %+v %v", got, err)
	}
}

func TestSlowSpotLeavesBudgetForOptionMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	spotCtx, stop := optionSpotContext(ctx)
	defer stop()
	<-spotCtx.Done()
	if ctx.Err() != nil {
		t.Fatal("spot lookup starved option collection")
	}
	po := &pendingOI{done: make(chan struct{})}
	po.setVolume(17)
	cancel()
	got, _ := waitOptionMetrics(ctx, po, make(chan error))
	if got.volume != 17 {
		t.Fatal("remaining collection result lost")
	}
}

func TestOptionJobsPrioritizeBothATMRights(t *testing.T) {
	jobs := []optionMetricJob{{80, "C", 0}, {90, "C", 1}, {100, "C", 2}, {110, "C", 3}, {80, "P", 0}, {100, "P", 1}}
	prioritizeOptionMetricJobs(jobs, 100)
	if len(jobs) != 6 || jobs[0].strike != 100 || jobs[0].right != "C" || jobs[1].strike != 100 || jobs[1].right != "P" {
		t.Fatalf("ATM sides not first: %+v", jobs)
	}
}

func TestOptionMetricsRetainErrorWhenDoneAlsoClosed(t *testing.T) {
	po := &pendingOI{done: make(chan struct{})}
	close(po.done)
	errs := make(chan error, 1)
	errs <- errors.New("broker failure")
	_, err := waitOptionMetrics(context.Background(), po, errs)
	if err == nil || err.Error() != "broker failure" {
		t.Fatalf("lost error: %v", err)
	}
}

func TestOptionSubscriptionNoticeAllowsDelayedTicks(t *testing.T) {
	w := newIbWrapper()
	po := w.registerOI(1004, "P")
	w.Error(1004, 0, 354, "not subscribed", "")
	select {
	case <-po.done:
		t.Fatal("notice ended collection before delayed ticks")
	default:
	}
	po.setVolume(8)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := waitOptionMetrics(ctx, po, make(chan error))
	if got.volume != 8 || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "354") {
		t.Fatalf("notice or ticks lost: %+v %v", got, err)
	}
}

func TestStressSampleWaitsForIVAfterOI(t *testing.T) {
	po := &pendingOI{done: make(chan struct{}), waitForMetrics: true}
	po.setOpenInterest(100)
	select {
	case <-po.done:
		t.Fatal("OI alone ended stress subscription before IV")
	default:
	}
	po.setVolume(20)
	po.setIV(.25)
	select {
	case <-po.done:
	default:
		t.Fatal("complete sample did not finish")
	}
}
