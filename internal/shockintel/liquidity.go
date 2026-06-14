package shockintel

import (
	"fmt"
	"math"
	"time"
)

type liquidityAsset struct {
	ID              string
	Label           string
	NormalSpreadBps float64
}

var liquidityAssets = []liquidityAsset{
	{ID: "SPY", Label: "SPY", NormalSpreadBps: 4.0},
	{ID: "QQQ", Label: "QQQ", NormalSpreadBps: 4.0},
	{ID: "IWM", Label: "IWM", NormalSpreadBps: 2.0},
	{ID: "TLT", Label: "TLT", NormalSpreadBps: 2.0},
	{ID: "HYG", Label: "HYG", NormalSpreadBps: 8.0},
	{ID: "LQD", Label: "LQD", NormalSpreadBps: 5.0},
}

func BuildLiquidityState(quotes map[string]ShockQuote, depth map[string]DepthSnapshot, asOf time.Time) LiquidityDTO {
	out := LiquidityDTO{AsOf: asOf.UTC(), Source: aggregateSource(quotes, "computed"), Rows: []LiquidityRow{}}
	for _, asset := range liquidityAssets {
		q, ok := quotes[asset.ID]
		row := LiquidityRow{ID: asset.ID, Label: asset.Label, NormalSpreadBps: asset.NormalSpreadBps, State: "missing"}
		if !ok {
			row.Missing = true
			row.Note = "quote missing"
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s: quote missing", asset.ID))
			out.Rows = append(out.Rows, row)
			continue
		}
		row.Label = nonEmpty(q.Label, asset.Label)
		row.Source = q.Source
		row.Basis = q.Basis
		row.AsOf = q.AsOf.UTC()
		row.Bid = q.Bid
		row.Ask = q.Ask
		row.TopBidSize = q.BidSize
		row.TopAskSize = q.AskSize
		if d, ok := depth[asset.ID]; ok && len(d.Levels) > 0 {
			row.DepthAvailable = true
			row.Top5BidDepth, row.Top5AskDepth = depthTotals(d.Levels, 5)
			if row.Bid <= 0 || row.Ask <= 0 {
				row.Bid, row.Ask = bestBidAsk(d.Levels)
			}
			if d.Source != "" {
				row.Source = d.Source
				row.Basis = d.Basis
				row.AsOf = d.AsOf.UTC()
			}
		} else {
			row.Note = "depth unavailable"
		}
		row.Mid = midpoint(ShockQuote{Price: q.Price, Bid: row.Bid, Ask: row.Ask})
		row.SpreadBps = spreadBps(ShockQuote{Price: q.Price, Bid: row.Bid, Ask: row.Ask})
		if row.SpreadBps <= 0 {
			row.Missing = true
			row.State = "missing"
			if row.Note == "" {
				row.Note = "bid/ask unavailable"
			} else {
				row.Note += "; bid/ask unavailable"
			}
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s: bid/ask unavailable", asset.ID))
			out.Rows = append(out.Rows, row)
			continue
		}
		row.SpreadRatio = spreadRatio(row.SpreadBps, asset.NormalSpreadBps)
		row.State = liquidityState(row.SpreadRatio, row.SpreadBps)
		out.Rows = append(out.Rows, row)
	}
	return out
}

func midpoint(q ShockQuote) float64 {
	if q.Bid > 0 && q.Ask > 0 {
		return (q.Bid + q.Ask) / 2
	}
	return q.Price
}

func spreadBps(q ShockQuote) float64 {
	if q.Bid <= 0 || q.Ask <= 0 {
		return 0
	}
	mid := midpoint(q)
	if mid <= 0 {
		return 0
	}
	return (q.Ask - q.Bid) / mid * 10000
}

func spreadRatio(spread, normal float64) float64 {
	if normal <= 0 || spread <= 0 {
		return 0
	}
	return math.Max(0, spread/normal)
}

func liquidityState(ratio, spread float64) string {
	switch {
	case ratio >= 8 || spread >= 75:
		return "severe"
	case ratio >= 3.5 || spread >= 20:
		return "stressed"
	case ratio >= 2.5:
		return "watch"
	default:
		return "normal"
	}
}

func depthTotals(levels []DepthLevel, n int) (float64, float64) {
	var bid, ask float64
	var bidN, askN int
	for _, level := range levels {
		switch level.Side {
		case "bid":
			if bidN < n {
				bid += level.Size
				bidN++
			}
		case "ask":
			if askN < n {
				ask += level.Size
				askN++
			}
		}
	}
	return bid, ask
}

func bestBidAsk(levels []DepthLevel) (float64, float64) {
	var bid, ask float64
	for _, level := range levels {
		switch level.Side {
		case "bid":
			if level.Price > bid {
				bid = level.Price
			}
		case "ask":
			if ask == 0 || level.Price < ask {
				ask = level.Price
			}
		}
	}
	return bid, ask
}
