package intraday

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IS908/optix/internal/portfolio"
	"github.com/IS908/optix/pkg/model"
)

type fakeSource struct {
	quotes map[string]Quote
	bars   map[string][]model.OHLCV
	err    error
}

func (f fakeSource) SourceName() string { return "ibkr" }

func (f fakeSource) Basis() string { return "realtime" }

func (f fakeSource) Quotes(_ context.Context, symbols []string) (map[string]Quote, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]Quote{}
	for _, symbol := range symbols {
		if q, ok := f.quotes[symbol]; ok {
			out[symbol] = q
		}
	}
	return out, nil
}

func (f fakeSource) Bars(_ context.Context, symbols []string, _ string, _ time.Duration) (map[string][]model.OHLCV, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string][]model.OHLCV{}
	for _, symbol := range symbols {
		if bars, ok := f.bars[symbol]; ok {
			out[symbol] = bars
		}
	}
	return out, nil
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
}

func bar(ts string, open, close float64, volume int64) model.OHLCV {
	t, _ := time.Parse(time.RFC3339, ts)
	return model.OHLCV{Timestamp: t, Open: open, High: close, Low: open, Close: close, Volume: volume}
}

func testSectors() *portfolio.SectorMap {
	return &portfolio.SectorMap{
		SectorLabels: map[string]string{"mega-cap-tech": "Mega-cap Tech", "etf": "ETF"},
		TickerSectors: map[string]string{
			"AAPL": "mega-cap-tech",
			"MSFT": "mega-cap-tech",
			"SPY":  "etf",
		},
	}
}

func TestMoversRanksGainersAndLosersFromSessionOpen(t *testing.T) {
	svc := NewService(fakeSource{
		quotes: map[string]Quote{
			"AAPL": {Symbol: "AAPL", Last: 110, AsOf: fixedNow()},
			"MSFT": {Symbol: "MSFT", Last: 95, AsOf: fixedNow()},
		},
		bars: map[string][]model.OHLCV{
			"AAPL": {bar("2026-06-25T13:30:00Z", 100, 101, 1000), bar("2026-06-25T14:00:00Z", 101, 109, 1500)},
			"MSFT": {bar("2026-06-25T13:30:00Z", 100, 99, 900), bar("2026-06-25T14:00:00Z", 99, 96, 1100)},
		},
	}, testSectors(), "<test>")
	svc.Now = fixedNow

	dto, err := svc.Movers(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dto.Gainers) == 0 || dto.Gainers[0].Symbol != "AAPL" {
		t.Fatalf("gainers = %#v, want AAPL first", dto.Gainers)
	}
	if len(dto.Losers) == 0 || dto.Losers[0].Symbol != "MSFT" {
		t.Fatalf("losers = %#v, want MSFT first", dto.Losers)
	}
	if got := dto.Gainers[0].Pct; got < 9.9 || got > 10.1 {
		t.Fatalf("AAPL pct = %.2f, want about 10", got)
	}
	if !dto.Gainers[0].Watchlist {
		t.Fatalf("AAPL watchlist = false, want true")
	}
	if dto.Source != "ibkr" || dto.Basis != "realtime" {
		t.Fatalf("source/basis = %s/%s, want ibkr/realtime", dto.Source, dto.Basis)
	}
}

func TestMoversDegradesWithWarningsAndNonNilSlices(t *testing.T) {
	svc := NewService(fakeSource{err: errors.New("ibkr down")}, testSectors(), "<test>")
	svc.Now = fixedNow

	dto, err := svc.Movers(context.Background(), []string{"AAPL"})
	if err != nil {
		t.Fatal(err)
	}
	if dto.Gainers == nil || dto.Losers == nil {
		t.Fatalf("movers arrays must be non-nil: %+v", dto)
	}
	if len(dto.Warnings) == 0 {
		t.Fatalf("warnings empty, want source failure warning")
	}
}

func TestSectorHeatmapAggregatesMoverRows(t *testing.T) {
	svc := NewService(fakeSource{
		quotes: map[string]Quote{
			"AAPL": {Symbol: "AAPL", Last: 110, AsOf: fixedNow()},
			"MSFT": {Symbol: "MSFT", Last: 98, AsOf: fixedNow()},
			"SPY":  {Symbol: "SPY", Last: 505, AsOf: fixedNow()},
		},
		bars: map[string][]model.OHLCV{
			"AAPL": {bar("2026-06-25T13:30:00Z", 100, 101, 1000)},
			"MSFT": {bar("2026-06-25T13:30:00Z", 100, 99, 1000)},
			"SPY":  {bar("2026-06-25T13:30:00Z", 500, 502, 1000)},
		},
	}, testSectors(), "<test>")
	svc.Now = fixedNow

	dto, err := svc.SectorHeatmap(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(dto.Rows) == 0 {
		t.Fatalf("heatmap rows empty")
	}
	tech := findSector(t, dto.Rows, "mega-cap-tech")
	if tech.SampleN != 2 || tech.Gainers != 1 || tech.Losers != 1 {
		t.Fatalf("tech row = %+v, want sample=2 gainers=1 losers=1", tech)
	}
	if tech.TopSymbol != "AAPL" {
		t.Fatalf("top symbol = %s, want AAPL", tech.TopSymbol)
	}
}

func findSector(t *testing.T, rows []SectorHeatmapRow, id string) SectorHeatmapRow {
	t.Helper()
	for _, row := range rows {
		if row.SectorID == id {
			return row
		}
	}
	t.Fatalf("sector %q not found in %#v", id, rows)
	return SectorHeatmapRow{}
}
