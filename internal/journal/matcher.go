// Package journal implements FIFO round-trip matching over a flat slice of
// executions. The matcher is a pure function — no I/O, no globals.
package journal

import (
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
	expiration string
	strike     float64
	right      string
}

func keyOf(e model.Execution) instrumentKey {
	return instrumentKey{e.Account, e.Symbol, e.SecType, e.Expiration, e.Strike, e.Right}
}

func multiplierFor(secType string) float64 {
	if secType == "OPT" {
		return 100
	}
	return 1
}

// MatchRoundTrips groups executions by instrument key, applies FIFO matching,
// and returns round trips deterministically ordered by (open_time, close_time,
// symbol). asOf decides which open option positions count as "expired".
func MatchRoundTrips(execs []model.Execution, asOf time.Time) []model.RoundTrip {
	groups := make(map[instrumentKey][]model.Execution)
	for _, e := range execs {
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
					Strike: k.strike, Right: k.right, Account: k.account,
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
					Strike: k.strike, Right: k.right, Account: k.account,
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
		Strike: k.strike, Right: k.right, Account: k.account,
		Direction: direction, OpenTime: lo.time,
		OpenQty: lo.qty, OpenAvgPrice: lo.avgPrice,
		Multiplier: mult, Status: "open",
		OpenExecIDs: []string{lo.execID},
	}
	if k.secType == "OPT" && k.expiration != "" {
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
