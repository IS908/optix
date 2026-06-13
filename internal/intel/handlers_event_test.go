package intel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/internal/eventintel"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

func TestEventEndpointsNil(t *testing.T) {
	h := &Handlers{}
	mux := http.NewServeMux()
	h.Register(mux)
	for _, p := range []string{"rates", "diff", "patterns", "sensitivity"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/intel/event/"+p, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("/%s nil service -> want 503, got %d", p, rec.Code)
		}
	}
}

func TestEventRatesEndpoint(t *testing.T) {
	svc := eventintel.NewService(staticEventSource{})
	svc.Now = func() time.Time { return eventNow() }
	h := &Handlers{Event: svc}
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/intel/event/rates", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"rows"`) || !strings.Contains(body, `"US10Y"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

type staticEventSource struct{}

func (staticEventSource) Quotes(context.Context, []string) (map[string]marketdata.Quote, error) {
	now := eventNow()
	return map[string]marketdata.Quote{
		"US2Y":  eventQuote("US2Y", "US 2Y", marketdata.ClassFuture, 4.1, 0.1, now),
		"US10Y": eventQuote("US10Y", "US 10Y", marketdata.ClassYield, 4.3, 0.2, now),
		"DXY":   eventQuote("DXY", "DXY", marketdata.ClassFX, 104, 1, now),
		"GOLD":  eventQuote("GOLD", "Gold", marketdata.ClassFuture, 2400, 12, now),
		"SPX":   eventQuote("SPX", "SPX", marketdata.ClassIndex, 5100, 50, now),
		"NDX":   eventQuote("NDX", "NDX", marketdata.ClassIndex, 18000, 120, now),
		"VIX":   eventQuote("VIX", "VIX", marketdata.ClassIndex, 16, -1, now),
	}, nil
}

func (staticEventSource) Bars(context.Context, []string, string, time.Duration) (map[string][]model.OHLCV, error) {
	return map[string][]model.OHLCV{
		"US10Y": {{Timestamp: eventNow().Add(-2 * time.Hour), Close: 4.0}},
		"SPX": {
			eventDailyBar(2026, 6, 9, 100),
			eventDailyBar(2026, 6, 10, 101),
			eventDailyBar(2026, 6, 11, 102),
		},
	}, nil
}

func (staticEventSource) Statements(context.Context) (eventintel.StatementFixture, eventintel.StatementFixture, error) {
	now := eventNow()
	return eventintel.StatementFixture{Title: "prior", Source: "test", PublishedAt: now.AddDate(0, -1, 0), Text: "Inflation has eased."},
		eventintel.StatementFixture{Title: "current", Source: "test", PublishedAt: now, Text: "Inflation remains elevated."}, nil
}

func (staticEventSource) EventDates(context.Context) ([]eventintel.EventDate, error) {
	return []eventintel.EventDate{{Date: eventDate(2026, 6, 10), Kind: "CPI", Label: "Jun CPI"}}, nil
}

func eventQuote(id, label string, class marketdata.AssetClass, price, change float64, now time.Time) marketdata.Quote {
	return marketdata.Quote{
		Ref:       marketdata.AssetRef{ID: id, Class: class},
		Label:     label,
		Price:     price,
		Change:    change,
		ChangePct: change / (price - change) * 100,
		AsOf:      now,
		Basis:     marketdata.BasisDelayed,
	}
}

func eventDailyBar(year int, month time.Month, day int, close float64) model.OHLCV {
	return model.OHLCV{Timestamp: eventDate(year, month, day), Close: close}
}

func eventDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func eventNow() time.Time {
	return time.Date(2026, 6, 13, 14, 0, 0, 0, time.UTC)
}
