package intel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/internal/shockintel"
	"github.com/IS908/optix/pkg/model"
)

func TestShockEndpointsNil(t *testing.T) {
	h := &Handlers{}
	mux := http.NewServeMux()
	h.Register(mux)
	for _, p := range []string{"regime", "fingerprint", "analogs", "liquidity"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/intel/shock/"+p, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("/%s nil service -> want 503, got %d", p, rec.Code)
		}
	}
}

func TestShockRegimeEndpoint(t *testing.T) {
	svc := shockintel.NewService(staticShockSource{})
	svc.Now = shockNow
	h := &Handlers{Shock: svc}
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/intel/shock/regime", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"state"`) || !strings.Contains(body, `"triggered_view"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

type staticShockSource struct{}

func (staticShockSource) Quotes(context.Context, []string) (map[string]shockintel.ShockQuote, error) {
	now := shockNow()
	return map[string]shockintel.ShockQuote{
		"VIX":   {ID: "VIX", Label: "VIX", Price: 29, ChangePct: 35, Source: "test", Basis: "realtime", AsOf: now},
		"SPY":   {ID: "SPY", Label: "SPY", Price: 500, ChangePct: -3, Bid: 499.9, Ask: 500.1, Source: "test", Basis: "realtime", AsOf: now},
		"QQQ":   {ID: "QQQ", Label: "QQQ", Price: 430, ChangePct: -3.5, Bid: 429.9, Ask: 430.1, Source: "test", Basis: "realtime", AsOf: now},
		"HYG":   {ID: "HYG", Label: "HYG", Price: 75, ChangePct: -1.5, Bid: 74.8, Ask: 75.2, Source: "test", Basis: "realtime", AsOf: now},
		"TLT":   {ID: "TLT", Label: "TLT", Price: 93, ChangePct: 2, Bid: 92.9, Ask: 93.1, Source: "test", Basis: "realtime", AsOf: now},
		"IWM":   {ID: "IWM", Label: "IWM", Price: 205, ChangePct: -2, Bid: 204.9, Ask: 205.1, Source: "test", Basis: "realtime", AsOf: now},
		"LQD":   {ID: "LQD", Label: "LQD", Price: 108, ChangePct: -1, Bid: 107.9, Ask: 108.1, Source: "test", Basis: "realtime", AsOf: now},
		"UUP":   {ID: "UUP", Label: "UUP", Price: 31, ChangePct: 1, Source: "test", Basis: "realtime", AsOf: now},
		"USO":   {ID: "USO", Label: "USO", Price: 80, ChangePct: -3, Source: "test", Basis: "realtime", AsOf: now},
		"GLD":   {ID: "GLD", Label: "GLD", Price: 220, ChangePct: 1, Source: "test", Basis: "realtime", AsOf: now},
		"US10Y": {ID: "US10Y", Label: "US 10Y", Price: 4.4, ChangePct: 0.8, Source: "test", Basis: "realtime", AsOf: now},
	}, nil
}

func (staticShockSource) Bars(context.Context, []string, string, time.Duration) (map[string][]model.OHLCV, error) {
	return map[string][]model.OHLCV{}, nil
}

func (staticShockSource) Depth(context.Context, []string, int) (map[string]shockintel.DepthSnapshot, error) {
	now := shockNow()
	return map[string]shockintel.DepthSnapshot{
		"SPY": {ID: "SPY", Source: "test", Basis: "realtime", AsOf: now, Levels: []shockintel.DepthLevel{
			{Side: "bid", Price: 499.9, Size: 1000},
			{Side: "ask", Price: 500.1, Size: 900},
		}},
	}, nil
}

func (staticShockSource) OptionMetrics(context.Context, []string) (map[string]shockintel.OptionStress, error) {
	return map[string]shockintel.OptionStress{}, nil
}

func shockNow() time.Time {
	return time.Date(2026, 6, 13, 15, 30, 0, 0, time.UTC)
}
