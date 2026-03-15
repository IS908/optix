package model

import "time"

// StockQuote represents a stock price quote.
type StockQuote struct {
	Symbol    string
	Last      float64
	Bid       float64
	Ask       float64
	Volume    int64
	Change    float64
	ChangePct float64
	High      float64
	Low       float64
	Open      float64
	Close     float64
	High52W   float64
	Low52W    float64
	AvgVolume float64
	Timestamp time.Time
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
