package model

import "time"

// OptionType represents call or put.
type OptionType int

const (
	OptionTypeCall OptionType = iota + 1
	OptionTypePut
)

func (o OptionType) String() string {
	switch o {
	case OptionTypeCall:
		return "CALL"
	case OptionTypePut:
		return "PUT"
	default:
		return "UNKNOWN"
	}
}

// Greeks represents option Greeks.
type Greeks struct {
	Delta float64
	Gamma float64
	Theta float64
	Vega  float64
	Rho   float64
}

// OptionQuote represents a single option contract quote.
type OptionQuote struct {
	Underlying        string
	Expiration        string // YYYY-MM-DD
	Strike            float64
	OptionType        OptionType
	Last              float64
	Bid               float64
	Ask               float64
	Mid               float64
	Volume            int64
	OpenInterest      int32
	ImpliedVolatility float64
	Greeks            Greeks
	Timestamp         time.Time
}

// OptionChainExpiry holds all options for a single expiration date.
type OptionChainExpiry struct {
	Expiration   string
	DaysToExpiry int
	Calls        []OptionQuote
	Puts         []OptionQuote
}

// OptionChain holds the full option chain for an underlying.
type OptionChain struct {
	Underlying      string
	UnderlyingPrice float64
	Expirations     []OptionChainExpiry
}
