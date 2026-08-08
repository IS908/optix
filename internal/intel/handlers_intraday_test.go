package intel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/internal/intraday"
	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/pkg/model"
)

func TestIntradayHandlersServeMoversAndSectorHeatmap(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	svc := intraday.NewService(staticIntradaySource{now: now}, &portfolio.SectorMap{
		SectorLabels:  map[string]string{"mega-cap-tech": "Mega-cap Tech"},
		TickerSectors: map[string]string{"AAPL": "mega-cap-tech"},
	}, "<test>")
	svc.Now = func() time.Time { return now }
	h := &Handlers{
		Intraday: svc,
		Watchlist: func(context.Context) ([]string, error) {
			return []string{"AAPL"}, nil
		},
	}
	mux := http.NewServeMux()
	h.Register(mux)

	for _, path := range []string{"/api/intel/intraday/movers", "/api/intel/intraday/sector-heatmap"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s invalid JSON: %v", path, err)
		}
		if body["source"] != "ibkr" || body["basis"] != "realtime" {
			t.Fatalf("%s source/basis = %v/%v, want ibkr/realtime", path, body["source"], body["basis"])
		}
	}
}

// TestIntradayHandlersAppendWatchlistWarningOnLoadFailure is the #191
// finding-6 regression test: a watchlist load error was silently swallowed
// (`watchlist, _ := h.Watchlist(ctx)`) rather than surfaced to the caller.
// The card should still render (degrading to the curated-only universe)
// but must say why the watchlist portion is missing.
func TestIntradayHandlersAppendWatchlistWarningOnLoadFailure(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	svc := intraday.NewService(staticIntradaySource{now: now}, &portfolio.SectorMap{
		SectorLabels:  map[string]string{"mega-cap-tech": "Mega-cap Tech"},
		TickerSectors: map[string]string{"AAPL": "mega-cap-tech"},
	}, "<test>")
	svc.Now = func() time.Time { return now }
	h := &Handlers{
		Intraday: svc,
		Watchlist: func(context.Context) ([]string, error) {
			return nil, errors.New("db locked")
		},
	}
	mux := http.NewServeMux()
	h.Register(mux)

	for _, path := range []string{"/api/intel/intraday/movers", "/api/intel/intraday/sector-heatmap"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
		var body struct {
			Warnings []string `json:"warnings"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s invalid JSON: %v", path, err)
		}
		found := false
		for _, w := range body.Warnings {
			if strings.Contains(w, "watchlist") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s warnings = %v, want a watchlist-load warning instead of a silent swallow", path, body.Warnings)
		}
	}
}

type staticIntradaySource struct{ now time.Time }

func (s staticIntradaySource) SourceName() string { return "ibkr" }

func (s staticIntradaySource) Basis() string { return "realtime" }

func (s staticIntradaySource) Quotes(context.Context, []string) (map[string]intraday.Quote, error) {
	return map[string]intraday.Quote{"AAPL": {Symbol: "AAPL", Last: 110, AsOf: s.now}}, nil
}

func (s staticIntradaySource) Bars(context.Context, []string, string, time.Duration) (map[string][]model.OHLCV, error) {
	return map[string][]model.OHLCV{"AAPL": {{
		Timestamp: time.Date(2026, 6, 25, 13, 30, 0, 0, time.UTC),
		Open:      100,
		Close:     101,
		Volume:    1000,
	}}}, nil
}
