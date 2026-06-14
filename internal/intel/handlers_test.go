package intel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/internal/eventintel"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/internal/shockintel"
)

type fakePulse struct {
	snap     *marketdata.PulseSnapshot
	err      error
	gotView  marketdata.View
	gotSpark bool
}

func (f *fakePulse) Snapshot(_ context.Context, v marketdata.View, spark bool) (*marketdata.PulseSnapshot, error) {
	f.gotView, f.gotSpark = v, spark
	return f.snap, f.err
}

func newTestMux(p PulseProvider, now time.Time) *http.ServeMux {
	h := &Handlers{Pulse: p, Now: func() time.Time { return now }}
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func newTestMuxWithHandlers(h *Handlers) *http.ServeMux {
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func get(t *testing.T, mux *http.ServeMux, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("non-JSON response (%d): %s", rec.Code, rec.Body.String())
	}
	return rec, body
}

func TestStateEndpoint(t *testing.T) {
	// 2026-11-27 半日 11:00 ET：intraday、early_close、下一切换 13:00 postclose
	mux := newTestMux(nil, dET(2026, 11, 27, 11, 0))
	rec, body := get(t, mux, "/api/intel/state")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	want := map[string]any{
		"phase": "intraday", "view": "intraday",
		"is_trading_day": true, "early_close": true,
		"next_phase": "postclose", "calendar_stale": false,
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("%s = %v, want %v", k, body[k], v)
		}
	}
	if !strings.HasPrefix(body["next_transition"].(string), "2026-11-27T13:00:00") {
		t.Errorf("next_transition = %v", body["next_transition"])
	}
	if !strings.Contains(body["now"].(string), "-05:00") { // 11 月已是 EST
		t.Errorf("now should carry ET offset: %v", body["now"])
	}
}

func TestStateClosedWeekend(t *testing.T) {
	mux := newTestMux(nil, dET(2026, 6, 13, 12, 0)) // 周六
	_, body := get(t, mux, "/api/intel/state")
	if body["phase"] != "closed" || body["view"] != "postclose" || body["is_trading_day"] != false {
		t.Errorf("weekend state wrong: %v", body)
	}
}

func TestStateEventOverride(t *testing.T) {
	eventSvc := eventintel.NewService(staticEventSource{})
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	eventSvc.Now = func() time.Time { return now }
	mux := newTestMuxWithHandlers(&Handlers{Event: eventSvc, Now: func() time.Time { return now }})

	rec, body := get(t, mux, "/api/intel/state")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if body["view"] != "event" || body["base_view"] != "intraday" {
		t.Fatalf("view override = view:%v base:%v body=%v", body["view"], body["base_view"], body)
	}
	override := body["view_override"].(map[string]any)
	if override["source"] != "event_calendar" || !strings.Contains(override["reason"].(string), "Jun CPI") {
		t.Fatalf("override = %#v", override)
	}
}

func TestStateShockOverrideTakesPriorityOverEvent(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	eventSvc := eventintel.NewService(staticEventSource{})
	eventSvc.Now = func() time.Time { return now }
	shockSvc := shockintel.NewService(staticShockSource{})
	shockSvc.Now = func() time.Time { return now }
	mux := newTestMuxWithHandlers(&Handlers{
		Event: eventSvc,
		Shock: shockSvc,
		Now:   func() time.Time { return now },
	})

	_, body := get(t, mux, "/api/intel/state")
	if body["view"] != "shock" || body["base_view"] != "intraday" {
		t.Fatalf("shock override should win, got body=%v", body)
	}
	override := body["view_override"].(map[string]any)
	if override["source"] != "shock_regime" || !strings.Contains(override["reason"].(string), "shock") {
		t.Fatalf("override = %#v", override)
	}
}

func TestPulseEndpoint(t *testing.T) {
	price := 100.0
	fp := &fakePulse{snap: &marketdata.PulseSnapshot{
		SnapshotAt: time.Now().UTC(), View: marketdata.ViewIntraday,
		Assets: []marketdata.PulseAsset{
			{Quote: marketdata.Quote{Ref: marketdata.AssetRef{ID: "SPX", Class: marketdata.ClassIndex},
				Label: "SPX", Price: price, ChangePct: 1.2, Basis: marketdata.BasisDelayed, Source: "yfinance"}},
			{Quote: marketdata.Quote{Ref: marketdata.AssetRef{ID: "SOX_PROXY", Class: marketdata.ClassStock},
				Label: "SOX (via SOXX pre-mkt)", PctOnly: true, ChangePct: -1.3, Basis: marketdata.BasisApprox, Source: "yfinance"}},
		},
		Missing: []string{"US10Y"}, Warnings: []string{"US10Y: timeout"},
	}}
	// 缺省 view：2026-06-12 周五 13:00 → intraday、view_inferred=true
	mux := newTestMux(fp, dET(2026, 6, 12, 13, 0))
	rec, body := get(t, mux, "/api/intel/pulse")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if fp.gotView != marketdata.ViewIntraday || fp.gotSpark {
		t.Errorf("Snapshot called with (%s, %v)", fp.gotView, fp.gotSpark)
	}
	if body["view_inferred"] != true {
		t.Error("default view must set view_inferred=true")
	}
	assets := body["assets"].([]any)
	a0 := assets[0].(map[string]any)
	// CLI 同构字段名（spec：逐字段同构）
	for _, k := range []string{"id", "class", "label", "price", "change", "change_pct", "basis", "source", "basis_note", "as_of"} {
		if _, ok := a0[k]; !ok {
			t.Errorf("asset missing field %q", k)
		}
	}
	if a0["source"] != "yfinance" || !strings.Contains(a0["basis_note"].(string), "delayed") {
		t.Fatalf("SPX source/basis_note = %v / %v", a0["source"], a0["basis_note"])
	}
	if a1 := assets[1].(map[string]any); a1["price"] != nil {
		t.Errorf("pctOnly price must be null, got %v", a1["price"])
	} else if !strings.Contains(a1["basis_note"].(string), "SOXX") {
		t.Errorf("pctOnly basis_note should explain SOXX proxy, got %v", a1["basis_note"])
	}
	if body["missing"].([]any)[0] != "US10Y" {
		t.Errorf("missing = %v", body["missing"])
	}
}

func TestPulseDefaultUsesResolvedOverride(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	eventSvc := eventintel.NewService(staticEventSource{})
	eventSvc.Now = func() time.Time { return now }
	fp := &fakePulse{snap: &marketdata.PulseSnapshot{View: marketdata.ViewEvent}}
	mux := newTestMuxWithHandlers(&Handlers{
		Pulse: fp,
		Event: eventSvc,
		Now:   func() time.Time { return now },
	})

	rec, body := get(t, mux, "/api/intel/pulse")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if fp.gotView != marketdata.ViewEvent {
		t.Fatalf("default pulse view = %s, want event", fp.gotView)
	}
	if body["view_inferred"] != true {
		t.Error("default resolved view must still be marked inferred")
	}
}

func TestPulseExplicitViewBypassesResolvedOverride(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	eventSvc := eventintel.NewService(staticEventSource{})
	eventSvc.Now = func() time.Time { return now }
	fp := &fakePulse{snap: &marketdata.PulseSnapshot{View: marketdata.ViewIntraday}}
	mux := newTestMuxWithHandlers(&Handlers{
		Pulse: fp,
		Event: eventSvc,
		Now:   func() time.Time { return now },
	})

	rec, body := get(t, mux, "/api/intel/pulse?view=intraday")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if fp.gotView != marketdata.ViewIntraday {
		t.Fatalf("explicit pulse view = %s, want intraday", fp.gotView)
	}
	if body["view_inferred"] != false {
		t.Error("explicit pulse view must not be marked inferred")
	}
}

func TestPulseExplicitAndInvalidView(t *testing.T) {
	fp := &fakePulse{snap: &marketdata.PulseSnapshot{View: marketdata.ViewShock}}
	mux := newTestMux(fp, dET(2026, 6, 12, 13, 0))
	rec, body := get(t, mux, "/api/intel/pulse?view=shock&spark=1")
	if rec.Code != 200 || fp.gotView != marketdata.ViewShock || !fp.gotSpark {
		t.Fatalf("explicit view: code=%d view=%s spark=%v", rec.Code, fp.gotView, fp.gotSpark)
	}
	if body["view_inferred"] != false {
		t.Error("explicit view must set view_inferred=false")
	}
	rec, body = get(t, mux, "/api/intel/pulse?view=bogus")
	if rec.Code != 400 || body["error"] == nil {
		t.Fatalf("invalid view: code=%d body=%v", rec.Code, body)
	}
}

func TestPulseEmptyAssetsSerializesAsArray(t *testing.T) {
	fp := &fakePulse{snap: &marketdata.PulseSnapshot{View: marketdata.ViewIntraday}}
	mux := newTestMux(fp, dET(2026, 6, 12, 13, 0))
	rec, _ := get(t, mux, "/api/intel/pulse")
	if !strings.Contains(rec.Body.String(), `"assets": []`) && !strings.Contains(rec.Body.String(), `"assets":[]`) {
		t.Errorf("empty assets must serialize as [], got: %s", rec.Body.String())
	}
}

func TestPulseNilProvider(t *testing.T) {
	mux := newTestMux(nil, dET(2026, 6, 12, 13, 0))
	rec, _ := get(t, mux, "/api/intel/pulse")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil provider should 503, got %d", rec.Code)
	}
}
