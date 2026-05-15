package ibkr

import (
	"strconv"
	"time"

	"github.com/IS908/optix/pkg/model"
	"github.com/scmhub/ibapi"
)

// parseMultiplier converts an ibapi Contract.Multiplier string to a float.
// Empty/unparseable falls back to 100 for options (the US equity-option
// standard) and 1 for everything else.
func parseMultiplier(s, secType string) float64 {
	if s != "" {
		if m, err := strconv.ParseFloat(s, 64); err == nil && m > 0 {
			return m
		}
	}
	if secType == "OPT" {
		return 100
	}
	return 1
}

// positionFromIB converts an ibapi Position callback's arguments into a
// model.Position. The mark-to-market fields are left zero — AccountService
// fills them later.
func positionFromIB(account string, c *ibapi.Contract, qty ibapi.Decimal, avgCost float64) model.Position {
	p := model.Position{
		Account:    account,
		Symbol:     c.Symbol,
		SecType:    c.SecType,
		Quantity:   qty.Float(),
		AvgCost:    avgCost,
		Multiplier: parseMultiplier(c.Multiplier, c.SecType),
	}
	if c.SecType == "OPT" {
		p.Expiration = c.LastTradeDateOrContractMonth
		p.Strike = c.Strike
		p.Right = c.Right
	}
	return p
}

// executionFromIB converts an ibapi ExecDetails callback's arguments into a
// model.Execution.
func executionFromIB(c *ibapi.Contract, e *ibapi.Execution) model.Execution {
	ex := model.Execution{
		ExecID:   e.ExecID,
		Time:     parseExecTime(e.Time),
		Account:  e.AcctNumber,
		Symbol:   c.Symbol,
		SecType:  c.SecType,
		Side:     e.Side,
		Shares:   e.Shares.Float(),
		Price:    e.Price,
		AvgPrice: e.AvgPrice,
		Exchange: e.Exchange,
		OrderID:  e.OrderID,
		PermID:   e.PermID,
	}
	if c.SecType == "OPT" {
		ex.Expiration = c.LastTradeDateOrContractMonth
		ex.Strike = c.Strike
		ex.Right = c.Right
	}
	return ex
}

// parseExecTime parses IBKR's execution-time string. The API has used a few
// formats across versions; we try the known layouts and return the zero time
// if none match (the CLI then renders "—" for that row).
func parseExecTime(s string) time.Time {
	layouts := []string{
		"20060102-15:04:05",
		"20060102  15:04:05",
		"20060102 15:04:05",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
