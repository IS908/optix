// Package portfolio implements account-level (vs single-name) risk views.
//
// Phase 1 ships concentration analysis: per-underlying weight rollup, sector
// grouping, top-N exposure, HHI, and threshold-based flagging. Future phases
// (greeks aggregation, stress test) will share the position-loading and
// aggregation primitives defined here.
package portfolio

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// Config controls concentration thresholds and display rollups. Zero values
// fall back to sensible defaults via DefaultConfig() — callers should compose
// overrides on top of that rather than zero-initializing.
type Config struct {
	WarnPct          float64 // single-name yellow flag threshold, percent of NLV
	RedPct           float64 // single-name red flag threshold
	TopN             int     // top-N rollup in the report
	Top2WarnPct      float64 // warn if Top 2 combined > this
	Top5WarnPct      float64 // warn if Top 5 combined > this
	HHIDiversifiedMax float64 // HHI×10000 below this counts as diversified
	HHIConcentratedMin float64 // HHI×10000 above this counts as concentrated
}

// DefaultConfig returns the Phase 1 default thresholds. They mirror the
// `concentration:` block in configs/portfolio.yaml so the two stay in sync.
func DefaultConfig() Config {
	return Config{
		WarnPct:           10.0,
		RedPct:            20.0,
		TopN:              10,
		Top2WarnPct:       30.0,
		Top5WarnPct:       60.0,
		HHIDiversifiedMax: 1500,
		HHIConcentratedMin: 2500,
	}
}

// SectorMap is the static ticker→sector mapping plus human-readable labels,
// loaded from configs/sectors.json. Missing tickers resolve to "unclassified".
type SectorMap struct {
	SectorLabels   map[string]string `json:"sector_labels"`
	TickerSectors  map[string]string `json:"ticker_sectors"`
}

// LoadSectorMap reads sectors.json from path. An empty path returns an empty
// map (every ticker becomes "unclassified") which keeps Compute usable in
// tests without filesystem touchpoints.
func LoadSectorMap(path string) (*SectorMap, error) {
	if path == "" {
		return &SectorMap{
			SectorLabels:  map[string]string{},
			TickerSectors: map[string]string{},
		}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sector map: %w", err)
	}
	var sm SectorMap
	if err := json.Unmarshal(data, &sm); err != nil {
		return nil, fmt.Errorf("parse sector map: %w", err)
	}
	if sm.SectorLabels == nil {
		sm.SectorLabels = map[string]string{}
	}
	if sm.TickerSectors == nil {
		sm.TickerSectors = map[string]string{}
	}
	return &sm, nil
}

// Sector returns the sector id for a ticker, or "unclassified" if missing.
func (sm *SectorMap) Sector(ticker string) string {
	if sm == nil {
		return "unclassified"
	}
	if s, ok := sm.TickerSectors[strings.ToUpper(ticker)]; ok {
		return s
	}
	return "unclassified"
}

// Label returns the human-readable label for a sector id, falling back to the
// id itself when no label is registered. This lets new sectors render
// reasonably even before the labels map is updated.
func (sm *SectorMap) Label(sectorID string) string {
	if sm == nil {
		return sectorID
	}
	if l, ok := sm.SectorLabels[sectorID]; ok {
		return l
	}
	if sectorID == "unclassified" {
		return "Unclassified"
	}
	return sectorID
}

// UnderlyingGroup is per-ticker aggregated exposure across stock + option legs.
// Weight is computed against the net-liq passed into Compute.
//
// AbsMarketValueUSD uses |MV| so that short options (negative MV) still register
// as economic exposure. NetMarketValueUSD preserves the sign for net P&L views.
type UnderlyingGroup struct {
	Ticker             string  `json:"ticker"`
	AbsMarketValueUSD  float64 `json:"abs_mkt_value_usd"`
	NetMarketValueUSD  float64 `json:"net_mkt_value_usd"`
	WeightPct          float64 `json:"weight_pct"`
	StockLegCount      int     `json:"stock_leg_count"`
	OptionLegCount     int     `json:"option_leg_count"`
	HasMissingMark     bool    `json:"has_missing_mark"`
}

// SectorGroup is per-sector aggregated exposure.
type SectorGroup struct {
	SectorID    string   `json:"sector_id"`
	SectorLabel string   `json:"sector_label"`
	AbsMarketValueUSD float64 `json:"abs_mkt_value_usd"`
	WeightPct   float64  `json:"weight_pct"`
	Tickers     []string `json:"tickers"`
}

// Flag is a threshold-triggered warning. Level is "warn" (yellow) or "red".
type Flag struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// Report is the full concentration snapshot. Marshals to a JSON shape suited
// for cron consumers; Render() produces the human-facing terminal view.
type Report struct {
	SnapshotAt        time.Time         `json:"snapshot_at"`
	NetLiqUSD         float64           `json:"net_liq_usd"`
	NetLiqSGD         float64           `json:"net_liq_sgd,omitempty"`
	FXUSDtoSGD        float64           `json:"fx_usd_to_sgd,omitempty"`
	DeployedUSD       float64           `json:"deployed_usd"`
	DeployedPct       float64           `json:"deployed_pct"`
	CashUSD           float64           `json:"cash_usd"`
	CashPct           float64           `json:"cash_pct"`

	Underlyings       []UnderlyingGroup `json:"underlyings"`        // sorted by weight desc
	Sectors           []SectorGroup     `json:"sectors"`            // sorted by weight desc

	Top2Pct           float64           `json:"top2_pct"`
	Top5Pct           float64           `json:"top5_pct"`
	TopNPct           float64           `json:"top_n_pct"`
	TopN              int               `json:"top_n"`
	HHI               float64           `json:"hhi"`                // weight²×10000 sum
	HHIBucket         string            `json:"hhi_bucket"`         // "diversified" | "moderate" | "concentrated"

	Flags             []Flag            `json:"flags"`

	// MissingMarkTickers lists tickers whose mark price was unavailable,
	// causing their weight to be under-reported. Surfaces the OPRA gap.
	MissingMarkTickers []string         `json:"missing_mark_tickers,omitempty"`

	UsedConfig        Config            `json:"used_config"`
}

// Compute aggregates positions into a Report. netLiqUSD must be > 0; if the
// caller can't read it from the broker yet, pass the sum of |MV| as a
// fallback (deployed_pct will then be 100%, which the renderer flags).
func Compute(positions []model.Position, netLiqUSD float64, cfg Config, sm *SectorMap) *Report {
	r := &Report{
		SnapshotAt: time.Now(),
		NetLiqUSD:  netLiqUSD,
		TopN:       cfg.TopN,
		UsedConfig: cfg,
	}
	if cfg.TopN <= 0 {
		r.TopN = DefaultConfig().TopN
	}

	// 1) Aggregate per underlying. The underlying ticker is model.Position.Symbol
	// for both stocks and options (IBKR convention). Options are summed by abs MV
	// to capture risk exposure (a short call's negative MV still represents real
	// directional/vol exposure).
	byTicker := map[string]*UnderlyingGroup{}
	var missing []string
	missingSet := map[string]struct{}{}
	for _, p := range positions {
		t := strings.ToUpper(p.Symbol)
		g, ok := byTicker[t]
		if !ok {
			g = &UnderlyingGroup{Ticker: t}
			byTicker[t] = g
		}
		if p.IsOption() {
			g.OptionLegCount++
		} else {
			g.StockLegCount++
		}
		// MarketValue is 0 when the mark is unavailable (option without OPRA).
		// Surface the gap rather than silently under-weight the position.
		if p.MarketValue == 0 && p.Quantity != 0 {
			g.HasMissingMark = true
			if _, seen := missingSet[t]; !seen {
				missingSet[t] = struct{}{}
				missing = append(missing, t)
			}
			continue
		}
		g.AbsMarketValueUSD += math.Abs(p.MarketValue)
		g.NetMarketValueUSD += p.MarketValue
	}
	sort.Strings(missing)
	r.MissingMarkTickers = missing

	// 2) Weights against NLV. Guard against zero/negative NLV.
	if netLiqUSD > 0 {
		for _, g := range byTicker {
			g.WeightPct = g.AbsMarketValueUSD / netLiqUSD * 100
		}
	}

	// 3) Sort and emit ordered slice.
	underlyings := make([]UnderlyingGroup, 0, len(byTicker))
	for _, g := range byTicker {
		underlyings = append(underlyings, *g)
	}
	sort.Slice(underlyings, func(i, j int) bool {
		return underlyings[i].AbsMarketValueUSD > underlyings[j].AbsMarketValueUSD
	})
	r.Underlyings = underlyings

	// 4) Deployed / cash split.
	for _, u := range underlyings {
		r.DeployedUSD += u.AbsMarketValueUSD
	}
	if netLiqUSD > 0 {
		r.DeployedPct = r.DeployedUSD / netLiqUSD * 100
		r.CashUSD = netLiqUSD - r.DeployedUSD
		if r.CashUSD < 0 {
			r.CashUSD = 0 // negative means leveraged or NLV stale; clamp for display
		}
		r.CashPct = r.CashUSD / netLiqUSD * 100
	}

	// 5) Top-K rollups.
	r.Top2Pct = sumTopK(underlyings, 2)
	r.Top5Pct = sumTopK(underlyings, 5)
	r.TopNPct = sumTopK(underlyings, r.TopN)

	// 6) HHI = Σ weight² × 10000 (industry standard scaling).
	for _, u := range underlyings {
		// weight is in percent (0–100), so dividing by 100 yields fraction 0–1.
		w := u.WeightPct / 100
		r.HHI += w * w
	}
	r.HHI *= 10000
	switch {
	case r.HHI <= cfg.HHIDiversifiedMax:
		r.HHIBucket = "diversified"
	case r.HHI >= cfg.HHIConcentratedMin:
		r.HHIBucket = "concentrated"
	default:
		r.HHIBucket = "moderate"
	}

	// 7) Sector rollup.
	bySector := map[string]*SectorGroup{}
	for _, u := range underlyings {
		sid := sm.Sector(u.Ticker)
		sg, ok := bySector[sid]
		if !ok {
			sg = &SectorGroup{SectorID: sid, SectorLabel: sm.Label(sid)}
			bySector[sid] = sg
		}
		sg.AbsMarketValueUSD += u.AbsMarketValueUSD
		sg.Tickers = append(sg.Tickers, u.Ticker)
	}
	sectors := make([]SectorGroup, 0, len(bySector))
	for _, sg := range bySector {
		if netLiqUSD > 0 {
			sg.WeightPct = sg.AbsMarketValueUSD / netLiqUSD * 100
		}
		sort.Strings(sg.Tickers)
		sectors = append(sectors, *sg)
	}
	sort.Slice(sectors, func(i, j int) bool {
		return sectors[i].AbsMarketValueUSD > sectors[j].AbsMarketValueUSD
	})
	r.Sectors = sectors

	// 8) Flags.
	r.Flags = flagAll(r, cfg)

	return r
}

func sumTopK(ug []UnderlyingGroup, k int) float64 {
	if k > len(ug) {
		k = len(ug)
	}
	var sum float64
	for i := 0; i < k; i++ {
		sum += ug[i].WeightPct
	}
	return sum
}

func flagAll(r *Report, cfg Config) []Flag {
	var out []Flag
	for _, u := range r.Underlyings {
		switch {
		case u.WeightPct > cfg.RedPct:
			out = append(out, Flag{
				Level:   "red",
				Message: fmt.Sprintf("%s %.1f%% > %.0f%% (red)", u.Ticker, u.WeightPct, cfg.RedPct),
			})
		case u.WeightPct > cfg.WarnPct:
			out = append(out, Flag{
				Level:   "warn",
				Message: fmt.Sprintf("%s %.1f%% > %.0f%% (yellow)", u.Ticker, u.WeightPct, cfg.WarnPct),
			})
		}
	}
	if r.Top2Pct > cfg.Top2WarnPct {
		out = append(out, Flag{
			Level:   "warn",
			Message: fmt.Sprintf("Top 2 %.1f%% > %.0f%% (warning)", r.Top2Pct, cfg.Top2WarnPct),
		})
	}
	if r.Top5Pct > cfg.Top5WarnPct {
		out = append(out, Flag{
			Level:   "warn",
			Message: fmt.Sprintf("Top 5 %.1f%% > %.0f%% (warning)", r.Top5Pct, cfg.Top5WarnPct),
		})
	}
	return out
}

// Render writes the human-facing concentration view to w. The format intentionally
// mirrors the existing `optix positions` tabular style for visual consistency.
func (r *Report) Render(w io.Writer) {
	fmt.Fprintln(w, "═══ PORTFOLIO CONCENTRATION ═══")
	fmt.Fprintf(w, "Snapshot: %s\n", r.SnapshotAt.Format("2006-01-02 15:04:05 MST"))
	if r.NetLiqSGD > 0 && r.FXUSDtoSGD > 0 {
		fmt.Fprintf(w, "Net Liq:  USD $%s  (SGD $%s, FX %.4f)\n",
			fmtMoney(r.NetLiqUSD), fmtMoney(r.NetLiqSGD), r.FXUSDtoSGD)
	} else {
		fmt.Fprintf(w, "Net Liq:  USD $%s\n", fmtMoney(r.NetLiqUSD))
	}
	if r.NetLiqUSD > 0 {
		fmt.Fprintf(w, "Deployed: $%s  (%.1f%%)  |  Cash: $%s (%.1f%%)\n\n",
			fmtMoney(r.DeployedUSD), r.DeployedPct,
			fmtMoney(r.CashUSD), r.CashPct)
	} else {
		fmt.Fprintln(w, "Deployed: <NLV unknown — pass --net-liq-usd>")
		fmt.Fprintln(w)
	}

	// Top-N table
	fmt.Fprintf(w, "Top %d Single-Name Exposure\n", r.TopN)
	fmt.Fprintf(w, "%-3s %-8s %12s %8s  %s\n", "#", "Ticker", "Mkt Value", "Weight", "Flag")
	fmt.Fprintln(w, strings.Repeat("─", 50))
	limit := r.TopN
	if limit > len(r.Underlyings) {
		limit = len(r.Underlyings)
	}
	for i := 0; i < limit; i++ {
		u := r.Underlyings[i]
		flag := flagSymbol(u.WeightPct, r.UsedConfig)
		fmt.Fprintf(w, "%-3d %-8s $%11s %7.1f%%  %s\n",
			i+1, u.Ticker, fmtMoney(u.AbsMarketValueUSD), u.WeightPct, flag)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Top 2: %.1f%%  |  Top 5: %.1f%%  |  HHI: %.0f (%s)\n\n",
		r.Top2Pct, r.Top5Pct, r.HHI, r.HHIBucket)

	// Sector
	if len(r.Sectors) > 0 {
		fmt.Fprintln(w, "Sector Concentration:")
		for _, s := range r.Sectors {
			fmt.Fprintf(w, "  %-32s %5.1f%%  (%s)\n",
				s.SectorLabel, s.WeightPct, strings.Join(s.Tickers, "/"))
		}
		fmt.Fprintln(w)
	}

	// Missing-mark warning (OPRA gap)
	if len(r.MissingMarkTickers) > 0 {
		fmt.Fprintf(w, "⚠️  Mark price missing for: %s\n", strings.Join(r.MissingMarkTickers, ", "))
		fmt.Fprintln(w, "   (weights for these tickers under-reported; needs OPRA subscription or IBKR MCP path)")
		fmt.Fprintln(w)
	}

	// Flags
	if len(r.Flags) > 0 {
		fmt.Fprintln(w, "⚠️  Thresholds breached:")
		for _, f := range r.Flags {
			marker := "•"
			if f.Level == "red" {
				marker = "🔴"
			}
			fmt.Fprintf(w, "   %s %s\n", marker, f.Message)
		}
	} else {
		fmt.Fprintln(w, "✅ No concentration thresholds breached.")
	}
}

func flagSymbol(weightPct float64, cfg Config) string {
	switch {
	case weightPct > cfg.RedPct:
		return fmt.Sprintf("🔴 >%.0f%%", cfg.RedPct)
	case weightPct > cfg.WarnPct:
		return fmt.Sprintf("⚠️  >%.0f%%", cfg.WarnPct)
	default:
		return ""
	}
}

// fmtMoney renders a USD amount with thousands separators and no decimal.
// Negative values keep the sign in front of the digits.
func fmtMoney(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	whole := int64(math.Round(v))
	s := fmt.Sprintf("%d", whole)
	// Insert commas every 3 digits from right.
	n := len(s)
	if n <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var b strings.Builder
	pre := n % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if n > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < n; i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < n {
			b.WriteByte(',')
		}
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
