package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/eventintel"
	"github.com/IS908/optix/internal/intel"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

func TestEventEndToEndAcceptance(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	svc := eventintel.NewService(webuiEventSource{})
	svc.Now = func() time.Time { return webuiEventNow() }

	srv := New(Config{Addr: "127.0.0.1:0"}, store)
	srv.AttachIntel(&intel.Handlers{Event: svc})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	for _, p := range []string{"rates", "diff", "patterns", "sensitivity"} {
		resp, err := http.Get(ts.URL + "/api/intel/event/" + p)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("/%s = %d", p, resp.StatusCode)
		}
		if body["as_of"] == nil {
			t.Errorf("/%s missing as_of: %v", p, body)
		}
	}

	resp, err := http.Get(ts.URL + "/api/intel/event/rates")
	if err != nil {
		t.Fatal(err)
	}
	var rates eventintel.RatePathDTO
	_ = json.NewDecoder(resp.Body).Decode(&rates)
	resp.Body.Close()
	if len(rates.Rows) == 0 {
		t.Fatalf("expected rate rows, got %+v", rates)
	}

	resp, err = http.Get(ts.URL + "/api/intel/event/diff")
	if err != nil {
		t.Fatal(err)
	}
	var diff eventintel.StatementDiffDTO
	_ = json.NewDecoder(resp.Body).Decode(&diff)
	resp.Body.Close()
	if diff.Verdict == "" {
		t.Fatalf("expected diff verdict, got %+v", diff)
	}

	resp, err = http.Get(ts.URL + "/intel/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /intel/ = %d", resp.StatusCode)
	}
}

type webuiEventSource struct{}

func (webuiEventSource) Quotes(context.Context, []string) (map[string]marketdata.Quote, error) {
	now := webuiEventNow()
	return map[string]marketdata.Quote{
		"US2Y":  webuiEventQuote("US2Y", "US 2Y", marketdata.ClassFuture, 4.1, 0.1, now),
		"US10Y": webuiEventQuote("US10Y", "US 10Y", marketdata.ClassYield, 4.3, 0.2, now),
		"DXY":   webuiEventQuote("DXY", "DXY", marketdata.ClassFX, 104, 1, now),
		"GOLD":  webuiEventQuote("GOLD", "Gold", marketdata.ClassFuture, 2400, 12, now),
		"SPX":   webuiEventQuote("SPX", "SPX", marketdata.ClassIndex, 5100, 50, now),
		"NDX":   webuiEventQuote("NDX", "NDX", marketdata.ClassIndex, 18000, 120, now),
		"VIX":   webuiEventQuote("VIX", "VIX", marketdata.ClassIndex, 16, -1, now),
	}, nil
}

func (webuiEventSource) Bars(context.Context, []string, string, time.Duration) (map[string][]model.OHLCV, error) {
	return map[string][]model.OHLCV{
		"US2Y":  webuiEventBars(4.0, 4.1),
		"US10Y": webuiEventBars(4.0, 4.2),
		"DXY":   webuiEventBars(100, 101),
		"GOLD":  webuiEventBars(2300, 2320),
		"SPX":   webuiEventBars(100, 101),
		"NDX":   webuiEventBars(100, 102),
		"VIX":   webuiEventBars(20, 18),
	}, nil
}

func (webuiEventSource) Statements(context.Context) (eventintel.StatementFixture, eventintel.StatementFixture, error) {
	now := webuiEventNow()
	return eventintel.StatementFixture{Title: "prior", Source: "test", PublishedAt: now.AddDate(0, -1, 0), Text: "Inflation has eased."},
		eventintel.StatementFixture{Title: "current", Source: "test", PublishedAt: now, Text: "Inflation remains elevated."}, nil
}

func (webuiEventSource) EventDates(context.Context) ([]eventintel.EventDate, error) {
	return []eventintel.EventDate{{Date: webuiEventDate(2026, 6, 10), Kind: "CPI", Label: "Jun CPI"}}, nil
}

func webuiEventQuote(id, label string, class marketdata.AssetClass, price, change float64, now time.Time) marketdata.Quote {
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

func webuiEventBars(prevClose, eventClose float64) []model.OHLCV {
	return []model.OHLCV{
		{Timestamp: webuiEventDate(2026, 6, 9), Close: prevClose},
		{Timestamp: webuiEventDate(2026, 6, 10), Close: eventClose},
		{Timestamp: webuiEventDate(2026, 6, 11), Close: eventClose * 1.01},
	}
}

func webuiEventDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func webuiEventNow() time.Time {
	return time.Date(2026, 6, 13, 14, 0, 0, 0, time.UTC)
}
