package model

import "time"

type SymbolBeta struct {
	Symbol       string
	Beta         float64
	Observations int
	AsOf         time.Time
	UpdatedAt    time.Time
}
