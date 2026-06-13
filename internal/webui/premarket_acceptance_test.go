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
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/internal/premarket"
	"github.com/IS908/optix/pkg/model"
)

func TestPremarketEndToEndAcceptance(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.UpsertGapStats(ctx, []model.PremarketGapStat{
		{Symbol: "SPX", Direction: "up", Band: "0.5-1", FillRate: 0.62, SampleN: 143, LookbackDays: 504, AsOf: time.Now().UTC()},
	}); err != nil {
		t.Fatal(err)
	}

	srv := New(Config{Addr: "127.0.0.1:0"}, store)
	srv.AttachIntel(&intel.Handlers{
		Premarket: premarket.NewService(webuiPremarketSource{}, store),
		Watchlist: func(context.Context) ([]string, error) {
			return []string{"AAPL"}, nil
		},
	})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	for _, p := range []string{"overnight", "gaps", "movers", "sentiment"} {
		resp, err := http.Get(ts.URL + "/api/intel/premarket/" + p)
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

	resp, err := http.Get(ts.URL + "/api/intel/premarket/overnight")
	if err != nil {
		t.Fatal(err)
	}
	var ov premarket.OvernightDTO
	_ = json.NewDecoder(resp.Body).Decode(&ov)
	resp.Body.Close()
	if ov.Consistency.Total != 4 {
		t.Errorf("overnight chain total = %d, want 4", ov.Consistency.Total)
	}

	for _, p := range []string{"/intel/", "/dashboard"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d", p, resp.StatusCode)
		}
	}
}

type webuiPremarketSource struct{}

func (webuiPremarketSource) Quotes(_ context.Context, ids []string) (map[string]marketdata.Quote, error) {
	m := map[string]marketdata.Quote{
		"VIX":     {Price: 20, Basis: marketdata.BasisDelayed, AsOf: time.Now().UTC()},
		"VIX3M":   {Price: 22, Basis: marketdata.BasisDelayed, AsOf: time.Now().UTC()},
		"ES":      {Price: 6100, ChangePct: 0.7, Basis: marketdata.BasisDelayed, AsOf: time.Now().UTC()},
		"N225":    {ChangePct: 1.0, Basis: marketdata.BasisDelayed, AsOf: time.Now().UTC()},
		"TSMC_TW": {ChangePct: 0.8, Basis: marketdata.BasisDelayed, AsOf: time.Now().UTC()},
		"SX5E":    {ChangePct: 0.5, Basis: marketdata.BasisDelayed, AsOf: time.Now().UTC()},
	}
	out := map[string]marketdata.Quote{}
	for _, id := range ids {
		if q, ok := m[id]; ok {
			out[id] = q
		}
	}
	return out, nil
}

func (webuiPremarketSource) DailyBars(context.Context, string, time.Duration) ([]model.OHLCV, error) {
	return nil, nil
}

func (webuiPremarketSource) PremarketBars(context.Context, []string, int) (map[string][]model.OHLCV, error) {
	return map[string][]model.OHLCV{}, nil
}

func (webuiPremarketSource) PutCallRatio(context.Context, string) (marketdata.PCRatio, error) {
	return marketdata.PCRatio{Underlying: "SPX", PCOI: 1.1, PCVol: 0.9}, nil
}
