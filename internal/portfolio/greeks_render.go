package portfolio

import (
	"fmt"
	"io"
	"strings"
)

// RenderGreeks writes the human-facing Greeks table. Units are in the header:
// Net Δ in delta-adjusted shares, Dollar Δ in USD per +1% spot, Γ as Δ-shares
// change per +1% spot, Vega in USD per +1% IV, Θ in USD per day.
func RenderGreeks(r *GreeksReport, w io.Writer) {
	fmt.Fprintf(w, "═══ PORTFOLIO GREEKS (by %s) ═══\n", r.GroupBy)
	fmt.Fprintf(w, "Snapshot: %s   ·   NLV %s   ·   r %.1f%%\n\n",
		r.SnapshotAt.Local().Format("2006-01-02 15:04:05 MST"),
		fmtMoney(r.NetLiqUSD), r.RiskFreeRate*100)

	fmt.Fprintf(w, "%-8s %12s %10s %12s %8s %10s %9s  %s\n",
		"Ticker", "Mkt Value", "Net Δ", "Dollar Δ", "Γ(/1%)", "Vega(/1%)", "Θ/day", "IV")
	fmt.Fprintf(w, "%-8s %12s %10s %12s %8s %10s %9s\n",
		"", "", "(shares)", "(/1% spot)", "", "($)", "($)")

	for _, g := range r.Groups {
		ivCol := g.IVSource
		if g.LegCount == 0 && g.SkippedLegCount > 0 {
			ivCol = "✗ skipped"
		}
		fmt.Fprintf(w, "%-8s %12s %+10.1f %12s %+8.2f %10s %9s  %s\n",
			g.Key, fmtMoney(g.MVUsd), g.NetDelta, fmtSignedMoney(g.DollarDelta),
			g.Gamma, fmtSignedMoney(g.Vega), fmtSignedMoney(g.Theta), ivCol)
	}
	fmt.Fprintln(w, strings.Repeat("─", 78))
	fmt.Fprintf(w, "%-8s %12s %+10.1f %12s %+8.2f %10s %9s\n",
		"TOTAL", "", r.Total.NetDelta, fmtSignedMoney(r.Total.DollarDelta),
		r.Total.Gamma, fmtSignedMoney(r.Total.Vega), fmtSignedMoney(r.Total.Theta))

	if len(r.SkippedLegs) > 0 {
		fmt.Fprintf(w, "\n⚠️  %d leg(s) skipped (no IV from chain or mark):\n", len(r.SkippedLegs))
		for _, s := range r.SkippedLegs {
			fmt.Fprintf(w, "   %s %s %s%.0f (%s)\n", s.Symbol, s.Expiration, s.Right, s.Strike, s.Reason)
		}
	}
}

// fmtSignedMoney renders a signed dollar amount with a leading sign, e.g. +$1,358.
func fmtSignedMoney(v float64) string {
	sign := "+"
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s$%s", sign, fmtMoney(v))
}
