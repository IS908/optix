package postclose

import (
	"math"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func pcBar(y, m, d, hh, mm int, close float64, vol int64) model.OHLCV {
	loc := nyLoc
	return model.OHLCV{
		Timestamp: time.Date(y, time.Month(m), d, hh, mm, 0, 0, loc).UTC(),
		Close:     close,
		Volume:    vol,
	}
}

func TestExtractPostcloseMovesComputesRegularAfterAndCombined(t *testing.T) {
	now := time.Date(2026, 6, 12, 18, 0, 0, 0, nyLoc)
	bars := map[string][]model.OHLCV{
		"AAPL": {
			pcBar(2026, 6, 11, 16, 0, 100, 1000),
			pcBar(2026, 6, 12, 15, 55, 105, 2000),
			pcBar(2026, 6, 12, 18, 0, 110, 500),
		},
	}
	got, warnings := ExtractPostcloseMoves(bars, map[string]bool{"AAPL": true}, now)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v", warnings)
	}
	if len(got) != 1 {
		t.Fatalf("moves = %+v", got)
	}
	move := got[0]
	if move.Symbol != "AAPL" || !move.Watchlist {
		t.Fatalf("identity = %+v", move)
	}
	if math.Abs(move.RegularPct-5.0) > 0.001 {
		t.Fatalf("regular pct = %.4f", move.RegularPct)
	}
	if math.Abs(move.AfterHoursPct-4.7619) > 0.001 {
		t.Fatalf("after pct = %.4f", move.AfterHoursPct)
	}
	if math.Abs(move.CombinedPct-10.0) > 0.001 {
		t.Fatalf("combined pct = %.4f", move.CombinedPct)
	}
}

func TestRankPostcloseMoversSplitsGainersAndLosers(t *testing.T) {
	in := []Mover{
		{Symbol: "AAPL", CombinedPct: 3.2},
		{Symbol: "NVDA", CombinedPct: -4.1},
		{Symbol: "MSFT", CombinedPct: 1.0},
	}
	g, l := rankPostcloseMovers(in)
	if len(g) == 0 || g[0].Symbol != "AAPL" {
		t.Fatalf("gainers = %+v", g)
	}
	if len(l) == 0 || l[0].Symbol != "NVDA" {
		t.Fatalf("losers = %+v", l)
	}
}
