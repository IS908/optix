package intraday

import "github.com/IS908/optix/internal/intelshared"

var curatedMovers = []string{"AAPL", "NVDA", "MSFT", "AMZN", "META", "GOOGL", "TSLA", "SPY", "QQQ"}

func moverUniverse(watchlist []string) (symbols []string, inWatchlist map[string]bool) {
	inWatchlist = map[string]bool{}
	seen := map[string]bool{}
	add := func(symbol string, watch bool) {
		normalized := intelshared.NormalizeSymbol(symbol)
		if normalized == "" {
			return
		}
		if watch {
			inWatchlist[normalized] = true
		}
		if seen[normalized] {
			return
		}
		seen[normalized] = true
		symbols = append(symbols, normalized)
	}
	for _, symbol := range watchlist {
		add(symbol, true)
	}
	for _, symbol := range curatedMovers {
		add(symbol, false)
	}
	return symbols, inWatchlist
}
