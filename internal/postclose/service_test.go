package postclose

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/pkg/model"
)

type fakeSource struct {
	earnings map[string][]marketdata.EarningsEvent
	bars     map[string][]model.OHLCV
	err      error
}

func (f fakeSource) Earnings(context.Context, []string, int) (map[string][]marketdata.EarningsEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.earnings, nil
}

func (f fakeSource) PostcloseBars(context.Context, []string, int) (map[string][]model.OHLCV, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.bars, nil
}

type countingSource struct {
	fakeSource
	barCalls int
}

func (f *countingSource) Earnings(ctx context.Context, symbols []string, limit int) (map[string][]marketdata.EarningsEvent, error) {
	return f.fakeSource.Earnings(ctx, symbols, limit)
}

func (f *countingSource) PostcloseBars(ctx context.Context, symbols []string, days int) (map[string][]model.OHLCV, error) {
	f.barCalls++
	return f.fakeSource.PostcloseBars(ctx, symbols, days)
}

func testSectorMap() *portfolio.SectorMap {
	return &portfolio.SectorMap{
		SectorLabels: map[string]string{"mega-cap-tech": "Mega-cap Tech"},
		TickerSectors: map[string]string{
			"AAPL": "mega-cap-tech",
			"MSFT": "mega-cap-tech",
		},
	}
}

func TestServiceDegradesSourceFailureWithNonNilSlices(t *testing.T) {
	svc := NewService(fakeSource{err: errors.New("source down")}, testSectorMap(), "<test>")
	svc.Now = func() time.Time { return time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC) }

	earnings, err := svc.Earnings(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	if earnings.Reports == nil || len(earnings.Warnings) == 0 {
		t.Fatalf("earnings degradation = %+v", earnings)
	}

	movers, err := svc.Movers(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	if movers.Gainers == nil || movers.Losers == nil || len(movers.Warnings) == 0 {
		t.Fatalf("movers degradation = %+v", movers)
	}
}

func TestServiceBundleBuildsAllCards(t *testing.T) {
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	src := fakeSource{
		earnings: map[string][]marketdata.EarningsEvent{
			"AAPL": {
				{Symbol: "AAPL", EventTime: now.Add(-time.Hour), Timing: "postmarket", EPSEstimate: fptr(1), EPSReported: fptr(1.1)},
			},
		},
		bars: map[string][]model.OHLCV{
			"AAPL": {
				pcBar(2026, 6, 11, 16, 0, 100, 1000),
				pcBar(2026, 6, 12, 15, 55, 105, 2000),
				pcBar(2026, 6, 12, 18, 0, 110, 500),
			},
			"MSFT": {
				pcBar(2026, 6, 11, 16, 0, 200, 1000),
				pcBar(2026, 6, 12, 15, 55, 201, 2000),
				pcBar(2026, 6, 12, 18, 0, 202, 500),
			},
		},
	}
	svc := NewService(src, testSectorMap(), "<test>")
	svc.Now = func() time.Time { return now }
	got, err := svc.Bundle(context.Background(), []string{"AAPL", "MSFT"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Earnings.Reports) != 1 {
		t.Fatalf("earnings = %+v", got.Earnings)
	}
	if len(got.Movers.Gainers) == 0 {
		t.Fatalf("movers = %+v", got.Movers)
	}
	if got.ReadAcross.Edges == nil || got.Timeline.Events == nil {
		t.Fatalf("nil slices in bundle = %+v", got)
	}
}

func TestServiceTimelineReusesMoverFetchForReadAcross(t *testing.T) {
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	src := &countingSource{fakeSource: fakeSource{
		earnings: map[string][]marketdata.EarningsEvent{
			"AAPL": {
				{Symbol: "AAPL", EventTime: now.Add(-time.Hour), Timing: "postmarket", EPSEstimate: fptr(1), EPSReported: fptr(1.1)},
			},
		},
		bars: map[string][]model.OHLCV{
			"AAPL": {
				pcBar(2026, 6, 11, 16, 0, 100, 1000),
				pcBar(2026, 6, 12, 15, 55, 105, 2000),
				pcBar(2026, 6, 12, 18, 0, 110, 500),
			},
			"MSFT": {
				pcBar(2026, 6, 11, 16, 0, 200, 1000),
				pcBar(2026, 6, 12, 15, 55, 201, 2000),
				pcBar(2026, 6, 12, 18, 0, 202, 500),
			},
		},
	}}
	svc := NewService(src, testSectorMap(), "<test>")
	svc.Now = func() time.Time { return now }

	got, err := svc.Timeline(context.Background(), []string{"AAPL", "MSFT"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) == 0 {
		t.Fatalf("timeline = %+v", got)
	}
	if src.barCalls != 1 {
		t.Fatalf("PostcloseBars calls = %d, want 1", src.barCalls)
	}
}

func TestServiceBundleReusesMoverFetchForReadAcross(t *testing.T) {
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	src := &countingSource{fakeSource: fakeSource{
		earnings: map[string][]marketdata.EarningsEvent{
			"AAPL": {
				{Symbol: "AAPL", EventTime: now.Add(-time.Hour), Timing: "postmarket", EPSEstimate: fptr(1), EPSReported: fptr(1.1)},
			},
		},
		bars: map[string][]model.OHLCV{
			"AAPL": {
				pcBar(2026, 6, 11, 16, 0, 100, 1000),
				pcBar(2026, 6, 12, 15, 55, 105, 2000),
				pcBar(2026, 6, 12, 18, 0, 110, 500),
			},
			"MSFT": {
				pcBar(2026, 6, 11, 16, 0, 200, 1000),
				pcBar(2026, 6, 12, 15, 55, 201, 2000),
				pcBar(2026, 6, 12, 18, 0, 202, 500),
			},
		},
	}}
	svc := NewService(src, testSectorMap(), "<test>")
	svc.Now = func() time.Time { return now }

	got, err := svc.Bundle(context.Background(), []string{"AAPL", "MSFT"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Timeline.Events) == 0 {
		t.Fatalf("bundle timeline = %+v", got.Timeline)
	}
	if src.barCalls != 1 {
		t.Fatalf("PostcloseBars calls = %d, want 1", src.barCalls)
	}
}
