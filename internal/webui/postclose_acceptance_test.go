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
	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/internal/postclose"
	"github.com/IS908/optix/pkg/model"
)

func TestPostcloseEndToEndAcceptance(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	svc := postclose.NewService(webuiPostcloseSource{}, webuiPostcloseSectorMap(), "<test>")
	svc.Now = func() time.Time { return time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC) }

	srv := New(Config{Addr: "127.0.0.1:0"}, store)
	srv.AttachIntel(&intel.Handlers{
		Postclose: svc,
		Watchlist: func(context.Context) ([]string, error) {
			return []string{"AAPL", "MSFT"}, nil
		},
	})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	for _, p := range []string{"earnings", "timeline", "read-across", "movers"} {
		resp, err := http.Get(ts.URL + "/api/intel/postclose/" + p)
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

	resp, err := http.Get(ts.URL + "/api/intel/postclose/read-across")
	if err != nil {
		t.Fatal(err)
	}
	var ra postclose.ReadAcrossDTO
	_ = json.NewDecoder(resp.Body).Decode(&ra)
	resp.Body.Close()
	if len(ra.Edges) == 0 {
		t.Fatalf("expected read-across edge, got %+v", ra)
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

type webuiPostcloseSource struct{}

func (webuiPostcloseSource) Earnings(context.Context, []string, int) (map[string][]marketdata.EarningsEvent, error) {
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	est := 1.0
	rep := 1.1
	return map[string][]marketdata.EarningsEvent{
		"AAPL": {{Symbol: "AAPL", EventTime: now.Add(-time.Hour), Timing: "postmarket", EPSEstimate: &est, EPSReported: &rep}},
	}, nil
}

func (webuiPostcloseSource) PostcloseBars(context.Context, []string, int) (map[string][]model.OHLCV, error) {
	loc := webuiPostcloseNY()
	return map[string][]model.OHLCV{
		"AAPL": {
			{Timestamp: time.Date(2026, 6, 11, 16, 0, 0, 0, loc).UTC(), Close: 100, Volume: 1000},
			{Timestamp: time.Date(2026, 6, 12, 15, 55, 0, 0, loc).UTC(), Close: 105, Volume: 1000},
			{Timestamp: time.Date(2026, 6, 12, 18, 0, 0, 0, loc).UTC(), Close: 110, Volume: 1000},
		},
		"MSFT": {
			{Timestamp: time.Date(2026, 6, 11, 16, 0, 0, 0, loc).UTC(), Close: 200, Volume: 1000},
			{Timestamp: time.Date(2026, 6, 12, 15, 55, 0, 0, loc).UTC(), Close: 201, Volume: 1000},
			{Timestamp: time.Date(2026, 6, 12, 18, 0, 0, 0, loc).UTC(), Close: 202, Volume: 1000},
		},
	}, nil
}

func webuiPostcloseSectorMap() *portfolio.SectorMap {
	return &portfolio.SectorMap{
		SectorLabels: map[string]string{"mega-cap-tech": "Mega-cap Tech"},
		TickerSectors: map[string]string{
			"AAPL": "mega-cap-tech",
			"MSFT": "mega-cap-tech",
		},
	}
}

func webuiPostcloseNY() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.FixedZone("EST", -5*3600)
	}
	return loc
}
