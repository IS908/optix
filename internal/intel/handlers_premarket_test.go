package intel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/internal/premarket"
	"github.com/IS908/optix/pkg/model"
)

func TestPremarketEndpointsNil(t *testing.T) {
	h := &Handlers{}
	mux := http.NewServeMux()
	h.Register(mux)
	for _, p := range []string{"overnight", "gaps", "movers", "sentiment"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/intel/premarket/"+p, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("/%s nil service -> want 503, got %d", p, rec.Code)
		}
	}
}

func TestPremarketSentimentEndpoint(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := premarket.NewService(staticPremarketSource{}, store)
	h := &Handlers{Premarket: svc}
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/intel/premarket/sentiment", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
}

type staticPremarketSource struct{}

func (staticPremarketSource) Quotes(_ context.Context, ids []string) (map[string]marketdata.Quote, error) {
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

func (staticPremarketSource) DailyBars(context.Context, string, time.Duration) ([]model.OHLCV, error) {
	return nil, nil
}

func (staticPremarketSource) PremarketBars(context.Context, []string, int) (map[string][]model.OHLCV, error) {
	return map[string][]model.OHLCV{}, nil
}

func (staticPremarketSource) PutCallRatio(context.Context, string) (marketdata.PCRatio, error) {
	return marketdata.PCRatio{Underlying: "SPX", PCOI: 1.1, PCVol: 0.9}, nil
}
