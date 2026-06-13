package shockintel

import (
	"math"
	"time"
)

func BuildRegimeTrigger(quotes map[string]ShockQuote, liquidity LiquidityDTO, asOf time.Time) RegimeDTO {
	out := RegimeDTO{
		AsOf:          asOf.UTC(),
		Source:        aggregateSource(quotes, "computed"),
		State:         "normal",
		Confirmations: []RegimeConfirmation{},
	}
	if vix, ok := quotes["VIX"]; ok {
		out.VIXSigma = math.Max(0, vix.ChangePct/10)
		out.Score += out.VIXSigma * 20
		if out.VIXSigma >= 1.5 {
			out.Confirmations = append(out.Confirmations, confirmation(vix, "volatility", 1.2, out.VIXSigma*12, "VIX shock"))
		}
	} else {
		out.Warnings = append(out.Warnings, "VIX quote missing")
	}
	for _, id := range []string{"SPY", "QQQ"} {
		if q, ok := quotes[id]; ok && q.ChangePct <= -1 {
			c := math.Min(math.Abs(q.ChangePct)*5, 22)
			out.Score += c
			out.Confirmations = append(out.Confirmations, confirmation(q, "equity", 1.0, c, "equity drawdown"))
		}
	}
	if q, ok := quotes["HYG"]; ok && q.ChangePct <= -0.75 {
		c := math.Min(math.Abs(q.ChangePct)*7, 18)
		out.Score += c
		out.Confirmations = append(out.Confirmations, confirmation(q, "credit", 1.0, c, "credit ETF weak"))
	}
	for _, id := range []string{"TLT", "UUP", "USO", "GLD"} {
		if q, ok := quotes[id]; ok && math.Abs(q.ChangePct) >= 1 {
			c := math.Min(math.Abs(q.ChangePct)*3, 12)
			out.Score += c
			out.Confirmations = append(out.Confirmations, confirmation(q, dimensionFor(id), 0.8, c, "cross-asset confirmation"))
		}
	}
	for _, row := range liquidity.Rows {
		if row.State == "stressed" || row.State == "severe" {
			c := math.Min(8+row.SpreadZ*3, 22)
			out.Score += c
			out.Confirmations = append(out.Confirmations, RegimeConfirmation{
				ID: row.ID, Label: nonEmpty(row.Label, row.ID), Dimension: "liquidity",
				Weight: 1.1, Contribution: c, Source: row.Source, Basis: row.Basis,
				AsOf: row.AsOf.UTC(), Note: "spread/depth stress",
			})
		}
	}
	out.Score = clamp(out.Score, 0, 100)
	switch {
	case out.Score >= 80:
		out.State = "critical"
	case out.Score >= 55:
		out.State = "shock"
	case out.Score >= 25:
		out.State = "watch"
	}
	if out.State == "shock" || out.State == "critical" {
		out.TriggeredView = "shock"
	}
	return out
}

func confirmation(q ShockQuote, dimension string, weight, contribution float64, note string) RegimeConfirmation {
	return RegimeConfirmation{
		ID: q.ID, Label: nonEmpty(q.Label, q.ID), Dimension: dimension, ChangePct: q.ChangePct,
		Weight: weight, Contribution: contribution, Source: q.Source, Basis: q.Basis, AsOf: q.AsOf.UTC(), Note: note,
	}
}

func dimensionFor(id string) string {
	switch id {
	case "TLT", "US10Y":
		return "rates"
	case "UUP":
		return "dollar"
	case "USO":
		return "oil"
	case "GLD":
		return "gold"
	default:
		return "cross_asset"
	}
}

func aggregateSource(quotes map[string]ShockQuote, fallback string) string {
	seen := map[string]struct{}{}
	for _, q := range quotes {
		if q.Source != "" {
			seen[q.Source] = struct{}{}
		}
	}
	if len(seen) == 1 {
		for s := range seen {
			return s
		}
	}
	if len(seen) > 1 {
		return "mixed"
	}
	return fallback
}

func nonEmpty(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}
