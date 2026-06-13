package intel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/internal/postclose"
	"github.com/IS908/optix/pkg/model"
)

func TestPostcloseEndpointsNil(t *testing.T) {
	h := &Handlers{}
	mux := http.NewServeMux()
	h.Register(mux)
	for _, p := range []string{"earnings", "timeline", "read-across", "movers"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/intel/postclose/"+p, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("/%s nil service -> want 503, got %d", p, rec.Code)
		}
	}
}

func TestPostcloseMoversEndpoint(t *testing.T) {
	svc := postclose.NewService(staticPostcloseSource{}, postcloseTestSectorMap(), "<test>")
	svc.Now = func() time.Time { return time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC) }
	h := &Handlers{
		Postclose: svc,
		Watchlist: func(context.Context) ([]string, error) {
			return []string{"AAPL"}, nil
		},
	}
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/intel/postclose/movers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() == "" || !strings.Contains(rec.Body.String(), `"gainers"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func postcloseTestSectorMap() *portfolio.SectorMap {
	return &portfolio.SectorMap{
		SectorLabels: map[string]string{"mega-cap-tech": "Mega-cap Tech"},
		TickerSectors: map[string]string{
			"AAPL": "mega-cap-tech",
			"MSFT": "mega-cap-tech",
		},
	}
}

type staticPostcloseSource struct{}

func (staticPostcloseSource) Earnings(context.Context, []string, int) (map[string][]marketdata.EarningsEvent, error) {
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	eps := 1.0
	rep := 1.1
	return map[string][]marketdata.EarningsEvent{
		"AAPL": {{Symbol: "AAPL", EventTime: now.Add(-time.Hour), Timing: "postmarket", EPSEstimate: &eps, EPSReported: &rep}},
	}, nil
}

func (staticPostcloseSource) PostcloseBars(context.Context, []string, int) (map[string][]model.OHLCV, error) {
	loc := postcloseNY()
	return map[string][]model.OHLCV{
		"AAPL": {
			{Timestamp: time.Date(2026, 6, 11, 16, 0, 0, 0, loc).UTC(), Close: 100, Volume: 1000},
			{Timestamp: time.Date(2026, 6, 12, 15, 55, 0, 0, loc).UTC(), Close: 105, Volume: 1000},
			{Timestamp: time.Date(2026, 6, 12, 18, 0, 0, 0, loc).UTC(), Close: 110, Volume: 1000},
		},
	}, nil
}

func postcloseNY() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.FixedZone("EST", -5*3600)
	}
	return loc
}
