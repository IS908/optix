package model

import "time"

// MarketSession represents the current US stock market trading session.
type MarketSession string

const (
	SessionPreMarket  MarketSession = "pre_market"  // 04:00–09:30 ET
	SessionRegular    MarketSession = "regular"      // 09:30–16:00 ET
	SessionPostMarket MarketSession = "post_market"  // 16:00–20:00 ET
	SessionClosed     MarketSession = "closed"       // 20:00–04:00 ET (next day), weekends, holidays
)

// USMarketSession returns the current market session based on US Eastern time.
func USMarketSession(now time.Time) MarketSession {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		// Fallback: assume UTC-5 (EST) if timezone database is unavailable.
		et = time.FixedZone("EST", -5*60*60)
	}
	t := now.In(et)

	// Weekends are always closed.
	wd := t.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return SessionClosed
	}

	hour, min, _ := t.Clock()
	mins := hour*60 + min // minutes since midnight

	switch {
	case mins >= 4*60 && mins < 9*60+30:
		return SessionPreMarket
	case mins >= 9*60+30 && mins < 16*60:
		return SessionRegular
	case mins >= 16*60 && mins < 20*60:
		return SessionPostMarket
	default:
		return SessionClosed
	}
}

// IsExtendedHours returns true if the session is pre-market or post-market.
func (s MarketSession) IsExtendedHours() bool {
	return s == SessionPreMarket || s == SessionPostMarket
}

// IsOpen returns true if the market is in any active trading session
// (pre-market, regular, or post-market).
func (s MarketSession) IsOpen() bool {
	return s != SessionClosed
}

// String returns a human-readable label for the session (Chinese).
func (s MarketSession) Label() string {
	switch s {
	case SessionPreMarket:
		return "盘前"
	case SessionRegular:
		return "盘中"
	case SessionPostMarket:
		return "盘后"
	default:
		return "休市"
	}
}

// StockQuote represents a stock price quote.
type StockQuote struct {
	Symbol        string
	Last          float64
	Bid           float64
	Ask           float64
	Volume        int64
	Change        float64
	ChangePct     float64
	High          float64
	Low           float64
	Open          float64
	Close         float64
	High52W       float64
	Low52W        float64
	AvgVolume     float64
	Timestamp     time.Time
	MarketSession MarketSession // current market session when quote was fetched
}

// OHLCV represents a single candlestick bar.
type OHLCV struct {
	Timestamp time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    int64
}
