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
	SnapshotAt time.Time              `json:"snapshot_at"`
	NetLiqUSD  float64                `json:"net_liq_usd"`
	Scenarios  []StressScenarioResult `json:"scenarios"`
}

func DefaultStressScenarios() []StressScenario {
	return []StressScenario{
		{ID: "spy-down-3", Label: "SPY -3%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.03}}},
		{ID: "spy-down-5", Label: "SPY -5%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.05}}},
		{ID: "spy-down-10", Label: "SPY -10%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.10}}},
		{ID: "vix-up-50", Label: "VIX +50%", Shocks: []StressShock{{Axis: "vix_pct", Magnitude: 0.50}}},
		{ID: "qqq-down-5", Label: "QQQ -5%", Shocks: []StressShock{{Axis: "qqq_pct", Magnitude: -0.05}}},
		{ID: "tech-correlated-5", Label: "Tech correlated -5%", Shocks: []StressShock{{Axis: "spy_pct", Magnitude: -0.03}, {Axis: "vix_pct", Magnitude: 0.30}}},
	}
}

// RunStress applies scenario shocks to an existing Greeks snapshot. It is a
// deterministic first-order risk view: DollarDelta and Gamma model price shock;
// Vega models VIX/IV shock. A later version can swap in full per-leg repricing
// while preserving this report shape and CLI contract.
func RunStress(g *GreeksReport, scenarios []StressScenario) *StressReport {
	if len(scenarios) == 0 {
		scenarios = DefaultStressScenarios()
	}
	r := &StressReport{SnapshotAt: g.SnapshotAt, NetLiqUSD: g.NetLiqUSD}
	for _, sc := range scenarios {
		res := StressScenarioResult{ID: sc.ID, Label: sc.Label, Shocks: append([]StressShock(nil), sc.Shocks...)}
		pricePct, ivPctPoints := stressAxes(sc.Shocks)
		for _, grp := range g.Groups {
			pnl := grp.DollarDelta*(pricePct*100) + 0.5*grp.Gamma*math.Pow(pricePct*100, 2) + grp.Vega*(ivPctPoints*100)
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

func stressAxes(shocks []StressShock) (pricePct float64, ivPct float64) {
	for _, sh := range shocks {
		switch sh.Axis {
		case "spy_pct", "qqq_pct", "underlying_pct":
			pricePct += sh.Magnitude
		case "vix_pct", "iv_pct":
			ivPct += sh.Magnitude
		}
	}
	return pricePct, ivPct
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
		fmt.Fprintf(w, "\n⚠️ Worst tail: %s costs %.1f%% of NLV\n", worst.Label, math.Abs(worst.PctNLV))
	}
}
