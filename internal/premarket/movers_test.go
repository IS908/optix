package premarket

import (
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func pbar(hhET int, vol int64, close float64) model.OHLCV {
	// 盘前窗 04:00-09:30 ET ≈ 08:00-13:30 UTC（EDT）。用 UTC hh 模拟。
	return model.OHLCV{Timestamp: time.Date(2026, 6, 12, hhET, 0, 0, 0, time.UTC), Close: close, Volume: vol}
}

func TestPremarketWindowVolume(t *testing.T) {
	// 仅 04:00-09:30 ET 窗内 bar 计入。EDT：08:00-13:30 UTC。
	bars := []model.OHLCV{
		pbar(7, 100, 10),  // 03:00 ET 窗前，不计
		pbar(8, 200, 11),  // 04:00 ET 窗内
		pbar(13, 300, 12), // 09:00 ET 窗内
		pbar(14, 400, 13), // 10:00 ET 窗后(盘中)，不计
	}
	vol, last := premarketWindow(bars)
	if vol != 500 || last != 12 {
		t.Errorf("window vol=%d last=%v, want 500 / 12", vol, last)
	}
}

func TestRankMovers(t *testing.T) {
	in := []moverInput{
		{Symbol: "AAPL", Pct: 2.1, VolRatio: 1.8, InWatchlist: true},
		{Symbol: "NVDA", Pct: -3.2, VolRatio: 2.5, InWatchlist: false},
		{Symbol: "MSFT", Pct: 0.4, VolRatio: 0.9, InWatchlist: false},
	}
	g, l := rankMovers(in)
	if len(g) == 0 || g[0].Symbol != "AAPL" {
		t.Errorf("top gainer = %+v", g)
	}
	if len(l) == 0 || l[0].Symbol != "NVDA" {
		t.Errorf("top loser = %+v", l)
	}
}

func TestCuratedSetNonEmpty(t *testing.T) {
	if len(curatedMovers) < 5 {
		t.Error("curated set should have a handful of liquid names")
	}
}

func TestMoverUniverseNormalizesInteriorWhitespace(t *testing.T) {
	symbols, inWL := moverUniverse([]string{" brk b ", "BRK\tB"})
	if len(symbols) == 0 || symbols[0] != "BRKB" {
		t.Fatalf("symbols = %#v, want BRKB first", symbols)
	}
	if !inWL["BRKB"] {
		t.Fatalf("watchlist map = %#v, want BRKB marked", inWL)
	}
}
