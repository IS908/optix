package ibkr

import (
	"log"
	"strconv"
	"strings"
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
		Currency:   c.Currency,
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
		Currency: c.Currency,
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
//
// Newer scmhub/ibapi versions deliver a trailing IANA timezone, e.g.
// "20260520 14:30:21 America/New_York". Go's time.Parse can't read IANA
// names directly, so we strip the suffix and re-attach the location via
// time.LoadLocation + time.ParseInLocation. Without this, every fill fell
// through to time.Time{}, which downstream --since filters then silently
// discarded — see #27.
//
// A no-layout-match is logged (not just returned silently) so the next
// format change surfaces in logs instead of as days of bogus 0001-01-01
// rows in the journal.
func parseExecTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}

	loc := time.UTC
	// Strip a trailing IANA-style timezone suffix (contains a '/', e.g.
	// "America/New_York"). Short codes like "EST" would parse natively via
	// the layout's "MST" verb, so we only intervene when '/' is present.
	if i := strings.LastIndexByte(s, ' '); i > 0 && strings.ContainsRune(s[i+1:], '/') {
		if l, err := time.LoadLocation(s[i+1:]); err == nil {
			loc = l
			s = strings.TrimSpace(s[:i])
		}
	}

	layouts := []string{
		"20060102-15:04:05",
		"20060102  15:04:05",
		"20060102 15:04:05",
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, loc); err == nil {
			return t
		}
	}
	log.Printf("ibkr: parseExecTime: no layout matched %q", s)
	return time.Time{}
}
