package portfolio

import (
	"fmt"
	"io"
	"math"
	"strings"
)

// RenderGreeks writes the human-facing Greeks table. Units are in the header:
// Net Δ in delta-adjusted shares, Dollar Δ in USD per +1% spot, Γ as Δ-shares
// change per +1% spot, Vega in USD per +1% IV, Θ in USD per day.
func RenderGreeks(r *GreeksReport, w io.Writer) {
	fmt.Fprintf(w, "═══ PORTFOLIO GREEKS (by %s) ═══\n", r.GroupBy)
	fmt.Fprintf(w, "Snapshot: %s   ·   NLV $%s   ·   r %.1f%%\n\n",
		r.SnapshotAt.Local().Format("2006-01-02 15:04:05 MST"),
		fmtMoney(r.NetLiqUSD), r.RiskFreeRate*100)

	fmt.Fprintf(w, "%-8s %12s %10s %12s %8s %11s %10s  %s\n",
		"Ticker", "Mkt Value", "Net Δ", "Dollar Δ", "Γ(/1%)", "Vega(/1%)", "Θ/day", "IV")
	fmt.Fprintf(w, "%-8s %12s %10s %12s %8s %11s %10s\n",
		"", "", "(shares)", "(/1% spot)", "", "($)", "($)")

	for _, g := range r.Groups {
		fmt.Fprintf(w, "%-8s %12s %+10.1f %12s %+8.2f %11s %10s  %s\n",
			g.Key, "$"+fmtMoney(g.MVUsd), g.NetDelta, fmtSignedMoney(g.DollarDelta),
			g.Gamma, fmtSignedMoney2(g.Vega), fmtSignedMoney2(g.Theta), ivLabel(g))
	}
	fmt.Fprintln(w, strings.Repeat("─", 80))
	fmt.Fprintf(w, "%-8s %12s %+10.1f %12s %+8.2f %11s %10s\n",
		"TOTAL", "", r.Total.NetDelta, fmtSignedMoney(r.Total.DollarDelta),
		r.Total.Gamma, fmtSignedMoney2(r.Total.Vega), fmtSignedMoney2(r.Total.Theta))

	if len(r.SkippedLegs) > 0 {
		fmt.Fprintf(w, "\n⚠️  %d leg(s) skipped (no IV from chain or mark):\n", len(r.SkippedLegs))
		for _, s := range r.SkippedLegs {
			fmt.Fprintf(w, "   %s %s %s%.0f (%s)\n", s.Symbol, s.Expiration, s.Right, s.Strike, s.Reason)
		}
	}
}

// ivLabel decorates a group's IV source for the table, per spec §5.1:
// chain ✓ (chain-provided), mark ~ (inverted from the mark), stock, mixed, or
// ✗ skipped when the group's only legs were all skipped.
func ivLabel(g GreeksGroup) string {
	if g.LegCount == 0 && g.SkippedLegCount > 0 {
		return "✗ skipped"
	}
	switch g.IVSource {
	case "chain":
		return "chain ✓"
	case "mark":
		return "mark ~"
	default:
		return g.IVSource // "stock", "mixed", or ""
	}
}

// fmtSignedMoney renders a signed whole-dollar amount with thousands
// separators and a leading sign, e.g. +$1,358. Used for Dollar Δ where cents
// are noise against a large base.
func fmtSignedMoney(v float64) string {
	sign := "+"
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s$%s", sign, fmtMoney(v))
}

// fmtSignedMoney2 renders a signed dollar amount to the cent, e.g. -$8.20.
// Used for Θ/day and Vega, where whole-dollar rounding would discard
// meaningful precision (a -$8.20 daily theta must not print as -$8).
func fmtSignedMoney2(v float64) string {
	sign := "+"
	if v < 0 {
		sign = "-"
		v = -v
	}
	whole := math.Floor(v)
	cents := int(math.Round((v - whole) * 100))
	if cents == 100 { // rounding carried into the next dollar
		whole++
		cents = 0
	}
	return fmt.Sprintf("%s$%s.%02d", sign, fmtMoney(whole), cents)
}
