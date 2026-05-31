package portfolio

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// OptionPricer is the aggregator's dependency on the Python Black-Scholes
// engine. Deliberately narrow — two pure-compute calls, no gRPC details — so
// tests inject a fake and Phase 3 stress reuses the identical pricing path.
type OptionPricer interface {
	PriceOption(ctx context.Context, spot, strike, tYears, r, iv float64, ot model.OptionType) (model.Greeks, error)
	// ImpliedVol inverts a mark to an IV. ok=false when it can't be trusted.
	ImpliedVol(ctx context.Context, mark, spot, strike, tYears, r float64, ot model.OptionType) (iv float64, ok bool, err error)
}

// ChainProvider fetches an option chain for one (underlying, expiration).
// Satisfied by the existing MarketDataService.
type ChainProvider interface {
	GetOptionChain(ctx context.Context, underlying, expiration string) (*model.OptionChain, error)
}

// GreeksGroup is per-underlying or per-sector aggregated, position-level dollar
// Greeks. All fields are in display units (see the design spec §4.3) so JSON
// and the rendered table never diverge.
type GreeksGroup struct {
	Key             string  `json:"key"`
	MVUsd           float64 `json:"mv_usd"`
	WeightPct       float64 `json:"weight_pct"`
	NetDelta        float64 `json:"net_delta"`    // delta-adjusted shares
	DollarDelta     float64 `json:"dollar_delta"` // USD per +1% spot
	Gamma           float64 `json:"gamma"`        // Δ-shares change per +1% spot
	Vega            float64 `json:"vega"`         // USD per +1% IV
	Theta           float64 `json:"theta"`        // USD per day
	LegCount        int     `json:"leg_count"`
	SkippedLegCount int     `json:"skipped_leg_count"`
	IVSource        string  `json:"iv_source,omitempty"` // chain|mark|stock|mixed
}

// SkippedLeg records an option leg excluded from the math, for transparency.
type SkippedLeg struct {
	Symbol     string  `json:"symbol"`
	Expiration string  `json:"expiration"`
	Right      string  `json:"right"`
	Strike     float64 `json:"strike"`
	Reason     string  `json:"reason"` // no_iv | no_mark | price_error | non_finite
}

// GreeksReport is the full snapshot. JSON shape mirrors concentration.Report.
type GreeksReport struct {
	SnapshotAt   time.Time     `json:"snapshot_at"`
	GroupBy      string        `json:"group_by"`
	NetLiqUSD    float64       `json:"net_liq_usd"`
	RiskFreeRate float64       `json:"risk_free_rate"`
	Groups       []GreeksGroup `json:"groups"`
	Total        GreeksGroup   `json:"total"`
	SkippedLegs  []SkippedLeg  `json:"skipped_legs,omitempty"`
}

func rightToOptionType(right string) model.OptionType {
	if strings.EqualFold(right, "P") {
		return model.OptionTypePut
	}
	return model.OptionTypeCall
}

// minTYears is the near-expiry floor. Below ~1 day, BS Greeks and mark→IV
// inversion are numerically unstable, so we skip rather than emit garbage.
const minTYears = 1.0 / 365.0

// heldLeg is a normalized option holding fed to pricing.
type heldLeg struct {
	Symbol     string
	Underlying string
	Expiration string // YYYYMMDD
	Strike     float64
	OptionType model.OptionType
	Right      string
	SignedQty  float64
	Multiplier float64
	Mark       float64 // per-share mark (Position.LastPrice)
	TYears     float64
}

// chainIV looks up a contract's implied volatility in a chain by strike+type.
// Returns (iv, true) only when a matching contract carries a positive IV.
//
// Precondition: chain is single-expiration. AggregateGreeks fetches the chain
// via GetOptionChain(underlying, leg.Expiration), which returns only the
// requested expiry, so matching on strike+type alone is unambiguous. If a
// future ChainProvider ever returns a multi-expiration chain, this would need
// an expiration filter to avoid picking a same-strike contract from the wrong
// expiry.
func chainIV(chain *model.OptionChain, strike float64, ot model.OptionType) (float64, bool) {
	if chain == nil {
		return 0, false
	}
	for _, exp := range chain.Expirations {
		quotes := exp.Calls
		if ot == model.OptionTypePut {
			quotes = exp.Puts
		}
		for _, q := range quotes {
			if q.OptionType == ot && math.Abs(q.Strike-strike) < 1e-6 {
				if q.ImpliedVolatility > 0 {
					return q.ImpliedVolatility, true
				}
				return 0, false
			}
		}
	}
	return 0, false
}

// resolveIV returns (iv, source, ok). source ∈ {"chain","mark"}.
//  1. chain contract IV (>0) → "chain"
//  2. else invert the held mark via pricer.ImpliedVol → "mark"
//  3. else ok=false → caller skips the leg
//
// Near-expiry (T <= minTYears) always skips.
func resolveIV(ctx context.Context, leg heldLeg, chain *model.OptionChain, spot, r float64, pricer OptionPricer) (float64, string, bool) {
	if leg.TYears <= minTYears {
		return 0, "", false
	}
	if iv, ok := chainIV(chain, leg.Strike, leg.OptionType); ok {
		return iv, "chain", true
	}
	if leg.Mark <= 0 {
		return 0, "", false
	}
	iv, ok, err := pricer.ImpliedVol(ctx, leg.Mark, spot, leg.Strike, leg.TYears, r, leg.OptionType)
	if err != nil || !ok || iv <= 0 {
		return 0, "", false
	}
	return iv, "mark", true
}

// GreeksOptions configures an aggregation run.
type GreeksOptions struct {
	GroupBy      string    // "underlying" | "sector"
	NetLiqUSD    float64   // weight denominator; if <=0 the caller should pass FallbackNLV
	RiskFreeRate float64   // r for Black-Scholes
	AsOf         time.Time // snapshot time; drives time-to-expiry
}

// greeksConcurrency bounds concurrent PriceOption calls, mirroring enrich.
const greeksConcurrency = 5

// pricedLeg is the per-leg result before grouping.
type pricedLeg struct {
	underlying  string
	mvUsd       float64
	netDelta    float64 // delta-adjusted shares
	dollarDelta float64
	gamma       float64 // display: Δ-shares per +1% spot
	vega        float64 // display: USD per +1% IV
	theta       float64 // display: USD per day
	ivSource    string  // chain|mark|stock
	skipped     *SkippedLeg
}

// AggregateGreeks computes position-level dollar Greeks grouped by underlying
// or sector. Non-USD and residual positions are excluded (Phase 1 predicates).
// Option legs whose IV can't be resolved are skipped and surfaced.
func AggregateGreeks(ctx context.Context, positions []model.Position, opts GreeksOptions,
	pricer OptionPricer, chains ChainProvider, sm *SectorMap) (*GreeksReport, error) {

	r := &GreeksReport{
		SnapshotAt: opts.AsOf, GroupBy: opts.GroupBy,
		NetLiqUSD: opts.NetLiqUSD, RiskFreeRate: opts.RiskFreeRate,
	}

	// 1) Filter + build held legs. Spot/chain fetched per (underlying, expiration).
	type chainKey struct{ underlying, expiration string }
	chainCache := map[chainKey]*model.OptionChain{}
	spotByUnderlying := map[string]float64{}

	var legs []heldLeg
	var stockLegs []model.Position
	for _, p := range positions {
		if isResidual(p) || isNonUSD(p) {
			continue
		}
		if p.SecType != "OPT" {
			stockLegs = append(stockLegs, p)
			continue
		}
		legs = append(legs, heldLeg{
			Symbol: p.Symbol, Underlying: strings.ToUpper(p.Symbol),
			Expiration: p.Expiration, Strike: p.Strike,
			OptionType: rightToOptionType(p.Right), Right: p.Right,
			SignedQty: p.Quantity, Multiplier: p.Multiplier,
			Mark: p.LastPrice, TYears: yearsToExpiry(p.Expiration, opts.AsOf),
		})
	}

	// 2) Fetch chains for distinct (underlying, expiration); cache spot.
	for _, leg := range legs {
		k := chainKey{leg.Underlying, leg.Expiration}
		if _, ok := chainCache[k]; ok {
			continue
		}
		ch, err := chains.GetOptionChain(ctx, leg.Underlying, leg.Expiration)
		if err != nil {
			ch = nil // fall through to mark-derived IV
		}
		chainCache[k] = ch
		if ch != nil && ch.UnderlyingPrice > 0 {
			spotByUnderlying[leg.Underlying] = ch.UnderlyingPrice
		}
	}

	// 3) Price option legs concurrently (bounded).
	priced := make([]pricedLeg, len(legs))
	sem := make(chan struct{}, greeksConcurrency)
	var wg sync.WaitGroup
	for i := range legs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				priced[idx] = pricedLeg{skipped: skipFor(legs[idx], "price_error")}
				return
			}
			defer func() { <-sem }()
			priced[idx] = priceOneLeg(ctx, legs[idx], chainCache[chainKey{legs[idx].Underlying, legs[idx].Expiration}],
				spotByUnderlying[legs[idx].Underlying], opts.RiskFreeRate, pricer)
		}(i)
	}
	wg.Wait()

	// 4) Aggregate. Stock legs first (delta = shares), then priced option legs.
	groups := map[string]*GreeksGroup{}
	keyOf := func(underlying string) string {
		if opts.GroupBy == "sector" {
			return sm.Sector(underlying)
		}
		return underlying
	}
	addGroup := func(key string) *GreeksGroup {
		g, ok := groups[key]
		if !ok {
			g = &GreeksGroup{Key: key}
			groups[key] = g
		}
		return g
	}
	for _, p := range stockLegs {
		g := addGroup(keyOf(strings.ToUpper(p.Symbol)))
		g.NetDelta += p.Quantity
		// Stock dollar-delta per +1% spot = NetDelta(shares) × price × 1%. For a
		// stock leg price = MarketValue/Quantity, so this is MarketValue × 0.01
		// — what the position gains on a +1% move. (Matches the option leg's
		// netDelta × spot × 0.01 so the column is consistent.)
		g.DollarDelta += p.MarketValue * 0.01
		g.MVUsd += math.Abs(p.MarketValue)
		g.LegCount++
		mergeIVSource(g, "stock")
	}
	for i, pl := range priced {
		key := keyOf(legs[i].Underlying)
		g := addGroup(key)
		if pl.skipped != nil {
			g.SkippedLegCount++
			r.SkippedLegs = append(r.SkippedLegs, *pl.skipped)
			continue
		}
		g.NetDelta += pl.netDelta
		g.DollarDelta += pl.dollarDelta
		g.Gamma += pl.gamma
		g.Vega += pl.vega
		g.Theta += pl.theta
		g.MVUsd += pl.mvUsd
		g.LegCount++
		mergeIVSource(g, pl.ivSource)
	}

	// 5) Weights, finalize, deterministic order.
	nlv := opts.NetLiqUSD
	out := make([]GreeksGroup, 0, len(groups))
	for _, g := range groups {
		if nlv > 0 {
			g.WeightPct = g.MVUsd / nlv * 100
		}
		out = append(out, *g)
		addToTotal(&r.Total, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MVUsd == out[j].MVUsd {
			return out[i].Key < out[j].Key
		}
		return out[i].MVUsd > out[j].MVUsd
	})
	// Sort with full tiebreak so the JSON snapshot is byte-stable across runs
	// (a strangle can skip two legs on the same symbol — Symbol alone isn't a
	// total order, and sort.Slice isn't stable).
	sort.Slice(r.SkippedLegs, func(i, j int) bool {
		a, b := r.SkippedLegs[i], r.SkippedLegs[j]
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		if a.Expiration != b.Expiration {
			return a.Expiration < b.Expiration
		}
		if a.Right != b.Right {
			return a.Right < b.Right
		}
		return a.Strike < b.Strike
	})
	r.Groups = out
	r.Total.Key = "TOTAL"
	return r, nil
}

func addToTotal(t *GreeksGroup, g GreeksGroup) {
	t.NetDelta += g.NetDelta
	t.DollarDelta += g.DollarDelta
	t.Gamma += g.Gamma
	t.Vega += g.Vega
	t.Theta += g.Theta
	t.MVUsd += g.MVUsd
	t.LegCount += g.LegCount
	t.SkippedLegCount += g.SkippedLegCount
}

func mergeIVSource(g *GreeksGroup, src string) {
	if g.IVSource == "" {
		g.IVSource = src
		return
	}
	if g.IVSource != src {
		g.IVSource = "mixed"
	}
}

func skipFor(leg heldLeg, reason string) *SkippedLeg {
	return &SkippedLeg{Symbol: leg.Symbol, Expiration: leg.Expiration, Right: leg.Right, Strike: leg.Strike, Reason: reason}
}

// priceOneLeg resolves IV, prices the leg, and converts to display-unit
// position Greeks. Returns a pricedLeg with skipped set on any failure.
func priceOneLeg(ctx context.Context, leg heldLeg, chain *model.OptionChain, spot, r float64, pricer OptionPricer) pricedLeg {
	if spot <= 0 {
		// No chain spot; mark-derived pricing needs a spot too, so skip.
		return pricedLeg{skipped: skipFor(leg, "no_mark")}
	}
	iv, src, ok := resolveIV(ctx, leg, chain, spot, r, pricer)
	if !ok {
		return pricedLeg{skipped: skipFor(leg, "no_iv")}
	}
	g, err := pricer.PriceOption(ctx, spot, leg.Strike, leg.TYears, r, iv, leg.OptionType)
	if err != nil {
		return pricedLeg{skipped: skipFor(leg, "price_error")}
	}
	if !allFinite(g) {
		return pricedLeg{skipped: skipFor(leg, "non_finite")}
	}
	scale := leg.SignedQty * leg.Multiplier
	netDelta := g.Delta * scale
	return pricedLeg{
		underlying:  leg.Underlying,
		mvUsd:       math.Abs(leg.Mark * scale),
		netDelta:    netDelta,
		dollarDelta: netDelta * spot * 0.01, // USD per +1% spot (not full notional)
		gamma:       g.Gamma * scale * spot * 0.01,
		vega:        g.Vega * scale / 100.0,
		theta:       g.Theta * scale,
		ivSource:    src,
	}
}

func allFinite(g model.Greeks) bool {
	for _, v := range []float64{g.Delta, g.Gamma, g.Vega, g.Theta} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// yearsToExpiry converts a YYYYMMDD expiration to years from asOf (ACT/365),
// flooring at 0 for already-expired dates.
func yearsToExpiry(yyyymmdd string, asOf time.Time) float64 {
	exp, err := time.Parse("20060102", yyyymmdd)
	if err != nil {
		return 0
	}
	// Expire at end-of-day UTC, matching the journal matcher convention.
	expEOD := time.Date(exp.Year(), exp.Month(), exp.Day(), 23, 59, 59, 0, time.UTC)
	d := expEOD.Sub(asOf).Hours() / 24.0 / 365.0
	if d < 0 {
		return 0
	}
	return d
}
