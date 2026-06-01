package portfolio

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

type StressShock struct {
	Axis      string  `json:"axis"`
	Magnitude float64 `json:"magnitude"`
}

type StressScenario struct {
	ID     string        `json:"id"`
	Label  string        `json:"label"`
	Shocks []StressShock `json:"shocks"`
}

type StressPositionPnL struct {
	Key    string  `json:"key"`
	PnLUSD float64 `json:"pnl_usd"`
}

type StressScenarioResult struct {
	ID            string              `json:"id"`
	Label         string              `json:"label"`
	Shocks        []StressShock       `json:"shocks"`
	TotalPnLUSD   float64             `json:"total_pnl_usd"`
	PctNLV        float64             `json:"pct_nlv"`
	WorstPosition StressPositionPnL   `json:"worst_position"`
	Positions     []StressPositionPnL `json:"positions"`
}

type StressReport struct {
	SnapshotAt      time.Time              `json:"snapshot_at"`
	NetLiqUSD       float64                `json:"net_liq_usd"`
	Scenarios       []StressScenarioResult `json:"scenarios"`
	SkippedLegCount int                    `json:"skipped_leg_count,omitempty"`
	SkippedLegs     []SkippedLeg           `json:"skipped_legs,omitempty"`
}

func DefaultStressScenarios() []StressScenario {
	return []StressScenario{
		{ID: "spy-down-3", Label: "SPY -3%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.03}}},
		{ID: "spy-down-5", Label: "SPY -5%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.05}}},
		{ID: "spy-down-10", Label: "SPY -10%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.10}}},
		{ID: "iv-up-5", Label: "IV +5 pts", Shocks: []StressShock{{Axis: "iv_points", Magnitude: 5.0}}},
		{ID: "qqq-down-5", Label: "QQQ -5%", Shocks: []StressShock{{Axis: "qqq_pct", Magnitude: -0.05}}},
		{ID: "tech-correlated-5", Label: "Tech correlated -5%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.03}, {Axis: "iv_points", Magnitude: 3.0}}},
	}
}

// RunStress applies scenario shocks to an existing Greeks snapshot. It is a
// deterministic first-order risk view: DollarDelta and DollarGamma model price
// shock; Vega models explicit IV-point shocks. A later version can swap in full per-leg repricing
// while preserving this report shape and CLI contract.
func RunStress(g *GreeksReport, scenarios []StressScenario) *StressReport {
	if len(scenarios) == 0 {
		scenarios = DefaultStressScenarios()
	}
	r := &StressReport{
		SnapshotAt:      g.SnapshotAt,
		NetLiqUSD:       g.NetLiqUSD,
		SkippedLegCount: len(g.SkippedLegs),
		SkippedLegs:     append([]SkippedLeg(nil), g.SkippedLegs...),
	}
	for _, sc := range scenarios {
		res := StressScenarioResult{ID: sc.ID, Label: sc.Label, Shocks: append([]StressShock(nil), sc.Shocks...)}
		for _, grp := range g.Groups {
			pricePct, ivPoints := stressAxesForGroup(grp.Key, sc.Shocks)
			pnl := grp.DollarDelta*(pricePct*100) + 0.5*grp.DollarGamma*math.Pow(pricePct*100, 2) + grp.Vega*ivPoints
			pos := StressPositionPnL{Key: grp.Key, PnLUSD: pnl}
			res.Positions = append(res.Positions, pos)
			res.TotalPnLUSD += pnl
			if res.WorstPosition.Key == "" || pnl < res.WorstPosition.PnLUSD {
				res.WorstPosition = pos
			}
		}
		if r.NetLiqUSD > 0 {
			res.PctNLV = res.TotalPnLUSD / r.NetLiqUSD * 100
		}
		sort.Slice(res.Positions, func(i, j int) bool {
			if res.Positions[i].PnLUSD == res.Positions[j].PnLUSD {
				return res.Positions[i].Key < res.Positions[j].Key
			}
			return res.Positions[i].PnLUSD < res.Positions[j].PnLUSD
		})
		r.Scenarios = append(r.Scenarios, res)
	}
	sort.Slice(r.Scenarios, func(i, j int) bool { return r.Scenarios[i].TotalPnLUSD < r.Scenarios[j].TotalPnLUSD })
	return r
}

func stressAxesForGroup(key string, shocks []StressShock) (pricePct float64, ivPoints float64) {
	for _, sh := range shocks {
		switch sh.Axis {
		case "underlying_pct":
			pricePct += sh.Magnitude
		case "spy_pct":
			pricePct += sh.Magnitude * stressBeta(key)
		case "qqq_pct":
			if isQQQShockTarget(key) {
				pricePct += sh.Magnitude * stressBeta(key)
			}
		case "iv_points":
			ivPoints += sh.Magnitude
		}
	}
	return pricePct, ivPoints
}

func stressBeta(key string) float64 {
	if beta, ok := defaultStressBetas[strings.ToUpper(key)]; ok {
		return beta
	}
	return 1
}

func isQQQShockTarget(key string) bool {
	k := strings.ToUpper(key)
	if defaultQQQStressTargets[k] {
		return true
	}
	return defaultQQQStressTargets[strings.ToLower(key)]
}

var defaultStressBetas = map[string]float64{
	// Static fallback betas keep config-driven stress from treating every
	// underlying as beta=1 until a historical symbol_beta cache is available.
	"AAPL":  1.2,
	"MSFT":  1.0,
	"GOOGL": 1.1,
	"GOOG":  1.1,
	"META":  1.2,
	"AMZN":  1.2,
	"NVDA":  1.8,
	"AMD":   1.7,
	"TSLA":  1.6,
	"KO":    0.5,
	"XOM":   0.8,
	"QQQ":   1.0,
	"SPY":   1.0,
}

var defaultQQQStressTargets = map[string]bool{
	"AAPL": true, "MSFT": true, "GOOGL": true, "GOOG": true, "META": true,
	"AMZN": true, "NVDA": true, "AMD": true, "AVGO": true, "QCOM": true,
	"INTC": true, "ADBE": true, "CRM": true, "TSLA": true, "NFLX": true,
	"QQQ":           true,
	"mega-cap-tech": true, "ai-chips": true, "software-cloud": true,
	"ai-auto-mobility": true, "ecommerce-consumer": true, "media-entertainment": true,
	"ai-interconnect": true,
}

func RenderStress(r *StressReport, w io.Writer) {
	fmt.Fprintln(w, "═══ PORTFOLIO STRESS TEST ═══")
	fmt.Fprintf(w, "Snapshot: %s  |  Base NLV: USD $%s\n\n",
		r.SnapshotAt.Local().Format("2006-01-02 15:04:05 MST"), fmtMoney(r.NetLiqUSD))
	fmt.Fprintf(w, "%-22s %12s %8s  %s\n", "Scenario", "Total P&L", "% NLV", "Worst Position")
	fmt.Fprintln(w, strings.Repeat("─", 62))
	for _, sc := range r.Scenarios {
		worst := "—"
		if sc.WorstPosition.Key != "" {
			worst = fmt.Sprintf("%s %s", sc.WorstPosition.Key, fmtSignedMoney(sc.WorstPosition.PnLUSD))
		}
		fmt.Fprintf(w, "%-22s %12s %7.1f%%  %s\n", sc.Label, fmtSignedMoney(sc.TotalPnLUSD), sc.PctNLV, worst)
	}
	fmt.Fprintln(w, strings.Repeat("─", 62))
	if len(r.Scenarios) > 0 {
		worst := r.Scenarios[0]
		switch {
		case worst.TotalPnLUSD < 0:
			fmt.Fprintf(w, "\n⚠️ Worst tail: %s costs %.1f%% of NLV\n", worst.Label, math.Abs(worst.PctNLV))
		case worst.TotalPnLUSD > 0:
			fmt.Fprintf(w, "\nLeast favorable: %s gains %.1f%% of NLV\n", worst.Label, worst.PctNLV)
		default:
			fmt.Fprintf(w, "\nLeast favorable: %s is flat vs NLV\n", worst.Label)
		}
	}
	if len(r.SkippedLegs) > 0 {
		fmt.Fprintf(w, "\n⚠️  %d leg(s) excluded from stress; stress may understate risk:\n", len(r.SkippedLegs))
		for _, s := range r.SkippedLegs {
			fmt.Fprintf(w, "   %s %s %s%.0f (%s)\n", s.Symbol, s.Expiration, s.Right, s.Strike, s.Reason)
		}
	}
}
