package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/intel"
	"github.com/IS908/optix/internal/shockintel"
	"github.com/IS908/optix/pkg/model"
)

func TestShockEndToEndAcceptance(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	svc := shockintel.NewService(webuiShockSource{})
	svc.Now = func() time.Time { return webuiShockNow() }

	srv := New(Config{Addr: "127.0.0.1:0"}, store)
	srv.AttachIntel(&intel.Handlers{Shock: svc})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	for _, p := range []string{"regime", "fingerprint", "analogs", "liquidity"} {
		resp, err := http.Get(ts.URL + "/api/intel/shock/" + p)
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

	resp, err := http.Get(ts.URL + "/api/intel/shock/regime")
	if err != nil {
		t.Fatal(err)
	}
	var regime shockintel.RegimeDTO
	_ = json.NewDecoder(resp.Body).Decode(&regime)
	resp.Body.Close()
	if regime.TriggeredView != "shock" {
		t.Fatalf("expected shock trigger, got %+v", regime)
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

type webuiShockSource struct{}

func (webuiShockSource) Quotes(context.Context, []string) (map[string]shockintel.ShockQuote, error) {
	now := webuiShockNow()
	return map[string]shockintel.ShockQuote{
		"VIX":   {ID: "VIX", Label: "VIX", Price: 29, ChangePct: 35, Source: "test", Basis: "realtime", AsOf: now},
		"SPY":   {ID: "SPY", Label: "SPY", Price: 500, ChangePct: -3, Bid: 499.9, Ask: 500.1, Source: "test", Basis: "realtime", AsOf: now},
		"QQQ":   {ID: "QQQ", Label: "QQQ", Price: 430, ChangePct: -3.5, Bid: 429.9, Ask: 430.1, Source: "test", Basis: "realtime", AsOf: now},
		"IWM":   {ID: "IWM", Label: "IWM", Price: 205, ChangePct: -2, Bid: 204.9, Ask: 205.1, Source: "test", Basis: "realtime", AsOf: now},
		"TLT":   {ID: "TLT", Label: "TLT", Price: 93, ChangePct: 2, Bid: 92.9, Ask: 93.1, Source: "test", Basis: "realtime", AsOf: now},
		"HYG":   {ID: "HYG", Label: "HYG", Price: 75, ChangePct: -1.5, Bid: 74.8, Ask: 75.2, Source: "test", Basis: "realtime", AsOf: now},
		"LQD":   {ID: "LQD", Label: "LQD", Price: 108, ChangePct: -1, Bid: 107.9, Ask: 108.1, Source: "test", Basis: "realtime", AsOf: now},
		"UUP":   {ID: "UUP", Label: "UUP", Price: 31, ChangePct: 1, Source: "test", Basis: "realtime", AsOf: now},
		"USO":   {ID: "USO", Label: "USO", Price: 80, ChangePct: -3, Source: "test", Basis: "realtime", AsOf: now},
		"GLD":   {ID: "GLD", Label: "GLD", Price: 220, ChangePct: 1, Source: "test", Basis: "realtime", AsOf: now},
		"US10Y": {ID: "US10Y", Label: "US 10Y", Price: 4.4, ChangePct: 0.8, Source: "test", Basis: "realtime", AsOf: now},
	}, nil
}

func (webuiShockSource) Bars(context.Context, []string, string, time.Duration) (map[string][]model.OHLCV, error) {
	return map[string][]model.OHLCV{}, nil
}

func (webuiShockSource) Depth(context.Context, []string, int) (map[string]shockintel.DepthSnapshot, error) {
	now := webuiShockNow()
	return map[string]shockintel.DepthSnapshot{
		"SPY": {ID: "SPY", Source: "test", Basis: "realtime", AsOf: now, Levels: []shockintel.DepthLevel{
			{Side: "bid", Price: 499.9, Size: 1000},
			{Side: "ask", Price: 500.1, Size: 900},
		}},
	}, nil
}

func (webuiShockSource) OptionMetrics(context.Context, []string) (map[string]shockintel.OptionStress, error) {
	return map[string]shockintel.OptionStress{}, nil
}

func webuiShockNow() time.Time {
	return time.Date(2026, 6, 13, 15, 30, 0, 0, time.UTC)
}
