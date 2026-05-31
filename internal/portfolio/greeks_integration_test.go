//go:build integration

package portfolio

import (
	"context"
	"math"
	"testing"

	"github.com/IS908/optix/internal/analysis"
	"github.com/IS908/optix/pkg/model"
)

// TestAggregate_RealPricerUnitConvention pins the unit convention against the
// real Python Black-Scholes engine: a known (S,K,T,r,IV) must produce
// display-unit Greeks with the right magnitudes and signs. Requires the Python
// server on localhost:50052 (make py-server).
func TestAggregate_RealPricerUnitConvention(t *testing.T) {
	ac, err := analysis.NewClient("localhost:50052")
	if err != nil {
		t.Fatalf("analysis client: %v", err)
	}
	defer ac.Close()
	pricer := analysis.GreeksPricer{Client: ac}

	// One long ATM call, chain supplies IV=0.25.
	pos := []model.Position{optionPos("AAA", "C", 1, 100, 5.0)}
	chain := mkChain("AAA", 100, model.OptionQuote{
		Strike: 100, OptionType: model.OptionTypeCall, ImpliedVolatility: 0.25,
	})
	r, err := AggregateGreeks(context.Background(), pos, greeksOpts("underlying"),
		pricer, fakeChains{"AAA": chain}, fakeSectorMap())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 1 {
		t.Fatalf("groups = %+v, want 1", r.Groups)
	}
	g := r.Groups[0]

	// ATM call delta ≈ 0.5/share → NetDelta in [40, 65] (×1×100). Sanity band.
	if g.NetDelta < 40 || g.NetDelta > 65 {
		t.Errorf("NetDelta = %v, want ATM call ~50", g.NetDelta)
	}
	// Long option theta is negative (time decay); vega positive.
	if g.Theta >= 0 {
		t.Errorf("long call theta should be negative, got %v", g.Theta)
	}
	if g.Vega <= 0 {
		t.Errorf("long call vega should be positive, got %v", g.Vega)
	}
	if math.IsNaN(g.Gamma) || math.IsInf(g.Gamma, 0) {
		t.Errorf("gamma not finite: %v", g.Gamma)
	}
	if g.IVSource != "chain" {
		t.Errorf("IVSource = %q, want chain", g.IVSource)
	}
}

// TestAggregate_RealMarkDerivedIV exercises the mark→IV fallback through the
// real ImpliedVol RPC: no chain, so IV is inverted from the mark.
func TestAggregate_RealMarkDerivedIV(t *testing.T) {
	ac, err := analysis.NewClient("localhost:50052")
	if err != nil {
		t.Fatalf("analysis client: %v", err)
	}
	defer ac.Close()
	pricer := analysis.GreeksPricer{Client: ac}

	// A chain with the wrong strike forces mark inversion; spot comes from the
	// chain's UnderlyingPrice.
	chain := mkChain("BBB", 100, model.OptionQuote{
		Strike: 999, OptionType: model.OptionTypeCall, ImpliedVolatility: 0.3,
	})
	pos := []model.Position{optionPos("BBB", "C", 1, 100, 5.0)}
	r, err := AggregateGreeks(context.Background(), pos, greeksOpts("underlying"),
		pricer, fakeChains{"BBB": chain}, fakeSectorMap())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 1 || r.Groups[0].IVSource != "mark" {
		t.Fatalf("expected one group with IVSource=mark, got %+v", r.Groups)
	}
	if r.Groups[0].NetDelta <= 0 {
		t.Errorf("long call NetDelta should be positive, got %v", r.Groups[0].NetDelta)
	}
}
