package model

import "time"

// MarketDepthLevel is one side/position row from an order book snapshot.
type MarketDepthLevel struct {
	Side     string
	Position int
	Price    float64
	Size     float64
}

// MarketDepth holds a bounded top-of-book depth snapshot for one symbol.
type MarketDepth struct {
	Symbol    string
	Timestamp time.Time
	Levels    []MarketDepthLevel
}
