// Package journal implements FIFO round-trip matching over a flat slice of
// executions. The matcher is a pure function — no I/O, no globals.
package journal

import (
	"math"
	"sort"
	"time"

	"github.com/IS908/optix/pkg/model"
)

type lot struct {
	time     time.Time
	qty      float64
	avgPrice float64
	execID   string
}

type instrumentKey struct {
	account    string
	symbol     string
	secType    string
	currency   string
	expiration string
	strike     float64
	right      string
}

func keyOf(e model.Execution) instrumentKey {
	return instrumentKey{
		account:    e.Account,
		symbol:     e.Symbol,
		secType:    e.SecType,
		currency:   model.NormalizeCurrency(e.Currency),
		expiration: e.Expiration,
		strike:     e.Strike,
		right:      e.Right,
	}
}

func multiplierFor(secType string) float64 {
	if secType == "OPT" || secType == "BAG" {
		return 100
	}
	return 1
}

// MatchRoundTrips groups executions by instrument key, applies FIFO matching,
// and returns round trips deterministically ordered by (open_time, close_time,
// symbol). asOf decides which open option positions count as "expired".
//
// Pre-processing pipeline (for issue #29 — BAG combo handling):
//  1. Group by PermID (IBKR's "one logical trade" identifier)
//  2. Within each group: if a BAG row is present, keep BAG only and inherit
//     max(legs.expiration); otherwise pass through unchanged.
//  3. Normalize BAG signed price → absolute value so the standard FIFO/PnL
//     formula works without special-casing. IBKR's Side (SLD/BOT) stays
//     authoritative — the price sign is informational and must NOT flip Side.
//
// After pre-processing, the existing instrumentKey grouping + matchGroup loop
// handles BAG and OPT/STK uniformly.
func MatchRoundTrips(execs []model.Execution, asOf time.Time) []model.RoundTrip {
	var processed []model.Execution
	for _, group := range groupByPermID(execs) {
		for _, e := range enrichBAGFromLegs(group) {
			processed = append(processed, normalizeBAGPrice(e))
		}
	}

	groups := make(map[instrumentKey][]model.Execution)
	for _, e := range processed {
		k := keyOf(e)
		groups[k] = append(groups[k], e)
	}
	var out []model.RoundTrip
	for k, group := range groups {
		sort.SliceStable(group, func(i, j int) bool { return group[i].Time.Before(group[j].Time) })
		out = append(out, matchGroup(k, group, asOf)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].OpenTime.Equal(out[j].OpenTime) {
			return out[i].OpenTime.Before(out[j].OpenTime)
		}
		if !out[i].CloseTime.Equal(out[j].CloseTime) {
			return out[i].CloseTime.Before(out[j].CloseTime)
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out
}

func matchGroup(k instrumentKey, execs []model.Execution, asOf time.Time) []model.RoundTrip {
	mult := multiplierFor(k.secType)
	var longOpens, shortOpens []lot
	var trips []model.RoundTrip

	for _, e := range execs {
		qty := e.Shares
		switch e.Side {
		case "BOT":
			for qty > 0 && len(shortOpens) > 0 {
				front := &shortOpens[0]
				take := qty
				if front.qty < take {
					take = front.qty
				}
				trips = append(trips, model.RoundTrip{
					Symbol: e.Symbol, SecType: k.secType, Expiration: k.expiration,
					Strike: k.strike, Right: k.right, Account: k.account, Currency: k.currency,
					Direction: "SHORT", OpenTime: front.time, CloseTime: e.Time,
					OpenQty: take, CloseQty: take,
					OpenAvgPrice: front.avgPrice, CloseAvgPrice: e.AvgPrice,
					Multiplier:  mult,
					RealizedPnL: (front.avgPrice - e.AvgPrice) * take * mult,
					HoldingDays: e.Time.Sub(front.time).Hours() / 24,
					Status:      "closed",
					OpenExecIDs: []string{front.execID}, CloseExecIDs: []string{e.ExecID},
				})
				front.qty -= take
				qty -= take
				if front.qty == 0 {
					shortOpens = shortOpens[1:]
				}
			}
			if qty > 0 {
				longOpens = append(longOpens, lot{e.Time, qty, e.AvgPrice, e.ExecID})
			}
		case "SLD":
			for qty > 0 && len(longOpens) > 0 {
				front := &longOpens[0]
				take := qty
				if front.qty < take {
					take = front.qty
				}
				trips = append(trips, model.RoundTrip{
					Symbol: e.Symbol, SecType: k.secType, Expiration: k.expiration,
					Strike: k.strike, Right: k.right, Account: k.account, Currency: k.currency,
					Direction: "LONG", OpenTime: front.time, CloseTime: e.Time,
					OpenQty: take, CloseQty: take,
					OpenAvgPrice: front.avgPrice, CloseAvgPrice: e.AvgPrice,
					Multiplier:  mult,
					RealizedPnL: (e.AvgPrice - front.avgPrice) * take * mult,
					HoldingDays: e.Time.Sub(front.time).Hours() / 24,
					Status:      "closed",
					OpenExecIDs: []string{front.execID}, CloseExecIDs: []string{e.ExecID},
				})
				front.qty -= take
				qty -= take
				if front.qty == 0 {
					longOpens = longOpens[1:]
				}
			}
			if qty > 0 {
				shortOpens = append(shortOpens, lot{e.Time, qty, e.AvgPrice, e.ExecID})
			}
		}
	}
	for _, lo := range longOpens {
		trips = append(trips, emitOpen(k, lo, "LONG", mult, asOf))
	}
	for _, so := range shortOpens {
		trips = append(trips, emitOpen(k, so, "SHORT", mult, asOf))
	}
	return trips
}

func emitOpen(k instrumentKey, lo lot, direction string, mult float64, asOf time.Time) model.RoundTrip {
	rt := model.RoundTrip{
		Symbol: k.symbol, SecType: k.secType, Expiration: k.expiration,
		Strike: k.strike, Right: k.right, Account: k.account, Currency: k.currency,
		Direction: direction, OpenTime: lo.time,
		OpenQty: lo.qty, OpenAvgPrice: lo.avgPrice,
		Multiplier: mult, Status: "open",
		OpenExecIDs: []string{lo.execID},
	}
	if (k.secType == "OPT" || k.secType == "BAG") && k.expiration != "" {
		if exp, err := time.Parse("20060102", k.expiration); err == nil {
			expEOD := time.Date(exp.Year(), exp.Month(), exp.Day(), 23, 59, 59, 0, time.UTC)
			if expEOD.Before(asOf) {
				rt.Status = "expired"
				rt.CloseTime = expEOD
				rt.HoldingDays = expEOD.Sub(lo.time).Hours() / 24
				if direction == "LONG" {
					rt.RealizedPnL = -lo.avgPrice * lo.qty * mult
				} else {
					rt.RealizedPnL = lo.avgPrice * lo.qty * mult
				}
			}
		}
	}
	return rt
}

// normalizeBAGPrice rewrites a BAG row's signed price into its absolute value
// so the matcher's standard FIFO + (open - close) × qty × mult formula
// computes spread P&L correctly without special-casing BAG.
//
// IBKR's Side (SLD/BOT) is the source of truth for whether you opened or
// closed the combo position. The price sign on BAG rows is informational
// (negative = net credit lifecycle, positive = net debit lifecycle) — it
// MUST NOT change Side, or BTC-of-a-credit-spread would mis-classify as
// opening another short.
//
// Non-BAG executions pass through unchanged.
func normalizeBAGPrice(e model.Execution) model.Execution {
	if e.SecType != "BAG" {
		return e
	}
	e.AvgPrice = math.Abs(e.AvgPrice)
	e.Price = math.Abs(e.Price)
	return e
}

// enrichBAGFromLegs collapses a PermID group into matching-ready form.
// If the group contains a BAG row, that row is returned with its Expiration
// set to max(legs.expiration) — so vertical/IC spreads inherit the single
// shared expiration, and calendar spreads anchor on the latest leg. The OPT
// leg rows are dropped from the matching path (still visible to users via
// `journal list`).
//
// If the group has no BAG row, it passes through unchanged (single-leg OPT,
// stock trades, etc).
func enrichBAGFromLegs(group []model.Execution) []model.Execution {
	var bagIdx = -1
	var legExps []string
	var legCurrency string
	for i, e := range group {
		if e.SecType == "BAG" {
			bagIdx = i
		} else if e.SecType == "OPT" && e.Expiration != "" {
			legExps = append(legExps, e.Expiration)
		}
		if e.SecType == "OPT" && legCurrency == "" && e.Currency != "" {
			legCurrency = e.Currency
		}
	}
	if bagIdx < 0 {
		return group
	}
	bag := group[bagIdx]
	if len(legExps) > 0 {
		sort.Strings(legExps)
		bag.Expiration = legExps[len(legExps)-1]
	}
	if bag.Currency == "" {
		bag.Currency = legCurrency
	}
	return []model.Execution{bag}
}

// groupByPermID partitions executions by IBKR PermID. PermID == 0 (old data
// or non-PermID brokers) falls back to one group per row — equivalent to
// today's behavior. Grouping is keyed by (Account, PermID) defensively:
// PermID is supposed to be globally unique per trade, but two accounts
// connected via different sessions could in principle produce the same
// numeric PermID. Account-keying prevents silent cross-account merging.
// The order of returned groups is intentionally undefined; callers must
// not rely on it.
func groupByPermID(execs []model.Execution) [][]model.Execution {
	type key struct {
		account string
		permID  int64
	}
	byKey := make(map[key][]model.Execution)
	var noID [][]model.Execution
	for _, e := range execs {
		if e.PermID == 0 {
			noID = append(noID, []model.Execution{e})
			continue
		}
		k := key{account: e.Account, permID: e.PermID}
		byKey[k] = append(byKey[k], e)
	}
	groups := make([][]model.Execution, 0, len(byKey)+len(noID))
	for _, g := range byKey {
		groups = append(groups, g)
	}
	groups = append(groups, noID...)
	return groups
}
