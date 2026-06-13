package shockintel

import (
	"math"
	"sort"
	"time"
)

func BuildShockVector(quotes map[string]ShockQuote) ShockVector {
	return ShockVector{
		Equity: averageChange(quotes, "SPY", "QQQ"),
		Vol:    changeOf(quotes, "VIX"),
		Credit: changeOf(quotes, "HYG"),
		Rates:  changeOf(quotes, "US10Y"),
		Dollar: changeOf(quotes, "UUP"),
		Oil:    changeOf(quotes, "USO"),
		Gold:   changeOf(quotes, "GLD"),
	}
}

func BuildShockAnalogs(vector ShockVector, templates []AnalogTemplate, asOf time.Time) AnalogsDTO {
	rows := make([]AnalogRow, 0, len(templates))
	for _, tmpl := range templates {
		sim, features := analogSimilarity(vector, tmpl.Vector)
		rows = append(rows, AnalogRow{
			Name: tmpl.Name, Date: tmpl.Date.UTC(), Category: tmpl.Category,
			Similarity: sim, NextSessionBias: tmpl.NextSessionBias, MatchedFeatures: features,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Similarity > rows[j].Similarity })
	return AnalogsDTO{AsOf: asOf.UTC(), Source: "local_shock_template_fixture", Rows: rows}
}

func defaultAnalogTemplates() []AnalogTemplate {
	return []AnalogTemplate{
		{
			Name: "COVID liquidity shock", Date: dateUTC(2020, 3, 12), Category: "liquidity",
			Vector:          ShockVector{Equity: -4.5, Vol: 38, Credit: -2.0, Rates: -1.0, Dollar: 0.5, Oil: -6.0, Gold: 1.2},
			NextSessionBias: "mixed",
		},
		{
			Name: "Inflation policy shock", Date: dateUTC(2022, 6, 13), Category: "policy",
			Vector:          ShockVector{Equity: -3.2, Vol: 22, Credit: -1.1, Rates: 1.3, Dollar: 1.1, Oil: 1.8, Gold: -0.7},
			NextSessionBias: "risk_off",
		},
		{
			Name: "Growth demand scare", Date: dateUTC(2018, 12, 24), Category: "demand",
			Vector:          ShockVector{Equity: -2.0, Vol: 18, Credit: -0.8, Rates: -0.2, Dollar: -0.4, Oil: -2.5, Gold: 0.3},
			NextSessionBias: "mixed",
		},
		{
			Name: "Oil supply shock", Date: dateUTC(2019, 9, 16), Category: "supply",
			Vector:          ShockVector{Equity: -0.6, Vol: 8, Credit: -0.2, Rates: 0.2, Dollar: 0.3, Oil: 12.0, Gold: 1.5},
			NextSessionBias: "mixed",
		},
	}
}

func analogSimilarity(a, b ShockVector) (float64, []string) {
	pairs := []struct {
		name  string
		x     float64
		y     float64
		scale float64
	}{
		{"equity", a.Equity, b.Equity, 6},
		{"vol", a.Vol, b.Vol, 45},
		{"credit", a.Credit, b.Credit, 3},
		{"rates", a.Rates, b.Rates, 2.5},
		{"dollar", a.Dollar, b.Dollar, 2.5},
		{"oil", a.Oil, b.Oil, 14},
		{"gold", a.Gold, b.Gold, 4},
	}
	var sum float64
	features := []string{}
	for _, p := range pairs {
		score := 1 - math.Min(math.Abs(p.x-p.y)/p.scale, 1)
		if p.x != 0 && p.y != 0 && sign(p.x) == sign(p.y) {
			score = math.Min(score+0.12, 1)
			features = append(features, p.name)
		}
		sum += score
	}
	return clamp(sum/float64(len(pairs)), 0, 1), features
}

func averageChange(quotes map[string]ShockQuote, ids ...string) float64 {
	var sum float64
	var n int
	for _, id := range ids {
		if q, ok := quotes[id]; ok {
			sum += q.ChangePct
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func changeOf(quotes map[string]ShockQuote, id string) float64 {
	if q, ok := quotes[id]; ok {
		return q.ChangePct
	}
	return 0
}

func sign(v float64) float64 {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	default:
		return 0
	}
}

func dateUTC(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
