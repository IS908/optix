package shockintel

import "time"

func BuildShockFingerprint(quotes map[string]ShockQuote, liquidity LiquidityDTO, options []OptionStress, asOf time.Time) FingerprintDTO {
	rows := []FingerprintRow{
		scoreSupply(quotes),
		scoreDemand(quotes),
		scoreLiquidity(quotes, liquidity, options),
		scorePolicy(quotes),
	}
	for i := range rows {
		rows[i].Score = clamp(rows[i].Score, 0, 100)
		rows[i].NormalizedScore = rows[i].Score / 100
		rows[i].Active = rows[i].Score >= 50
		if rows[i].Evidence == nil {
			rows[i].Evidence = []string{}
		}
		if rows[i].Missing == nil {
			rows[i].Missing = []string{}
		}
	}
	return FingerprintDTO{AsOf: asOf.UTC(), Source: aggregateSource(quotes, "computed"), Rows: rows, OptionStress: options}
}

func scoreSupply(q map[string]ShockQuote) FingerprintRow {
	row := FingerprintRow{Kind: "supply", Label: "Supply shock", Evidence: []string{}, Missing: []string{}}
	addIf(&row, q, "USO", 3, true, 35, "oil up")
	addIf(&row, q, "US10Y", 0.75, true, 25, "rates up")
	addIf(&row, q, "UUP", 0.75, true, 20, "dollar up")
	addIf(&row, q, "GLD", 0.5, true, 10, "gold firm")
	return row
}

func scoreDemand(q map[string]ShockQuote) FingerprintRow {
	row := FingerprintRow{Kind: "demand", Label: "Demand shock", Evidence: []string{}, Missing: []string{}}
	addIf(&row, q, "SPY", -1, false, 25, "equities down")
	addIf(&row, q, "QQQ", -1, false, 20, "growth down")
	addIf(&row, q, "HYG", -0.75, false, 25, "credit weak")
	addIf(&row, q, "USO", -2, false, 20, "oil down")
	return row
}

func scoreLiquidity(q map[string]ShockQuote, liquidity LiquidityDTO, options []OptionStress) FingerprintRow {
	row := FingerprintRow{Kind: "liquidity", Label: "Liquidity shock", Evidence: []string{}, Missing: []string{}}
	addIf(&row, q, "VIX", 10, true, 30, "VIX up")
	addIf(&row, q, "HYG", -0.75, false, 25, "credit ETF weak")
	for _, liq := range liquidity.Rows {
		if liq.State == "stressed" || liq.State == "severe" || liq.SpreadRatio >= 3.5 {
			row.Score += 35
			row.Evidence = append(row.Evidence, liq.ID+" spread/depth stressed")
			break
		}
	}
	if len(liquidity.Rows) == 0 {
		row.Missing = append(row.Missing, "liquidity")
	}
	for _, stress := range options {
		if stress.IVSkew >= 0.05 {
			row.Score += 20
			row.Evidence = append(row.Evidence, stress.Underlying+" option IV skew elevated")
			break
		}
	}
	return row
}

func scorePolicy(q map[string]ShockQuote) FingerprintRow {
	row := FingerprintRow{Kind: "policy", Label: "Policy shock", Evidence: []string{}, Missing: []string{}}
	addIf(&row, q, "US10Y", 0.75, true, 35, "rates up")
	addIf(&row, q, "UUP", 0.75, true, 25, "dollar up")
	addIf(&row, q, "SPY", -1, false, 20, "equities down")
	addIf(&row, q, "QQQ", -1, false, 20, "growth down")
	return row
}

func addIf(row *FingerprintRow, quotes map[string]ShockQuote, id string, threshold float64, above bool, points float64, evidence string) {
	q, ok := quotes[id]
	if !ok {
		row.Missing = append(row.Missing, id)
		return
	}
	if above && q.ChangePct >= threshold {
		row.Score += points
		row.Evidence = append(row.Evidence, evidence)
	}
	if !above && q.ChangePct <= threshold {
		row.Score += points
		row.Evidence = append(row.Evidence, evidence)
	}
}
