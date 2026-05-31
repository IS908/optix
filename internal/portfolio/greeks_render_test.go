package portfolio

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestFmtSignedMoney2_KeepsCents(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{-8.20, "-$8.20"},
		{-24.50, "-$24.50"},
		{0, "+$0.00"},
		{1234.5, "+$1,234.50"},
		{-156.999, "-$157.00"}, // rounds, carries into the next dollar
	}
	for _, c := range cases {
		if got := fmtSignedMoney2(c.in); got != c.want {
			t.Errorf("fmtSignedMoney2(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIVLabel_Decorations(t *testing.T) {
	cases := []struct {
		g    GreeksGroup
		want string
	}{
		{GreeksGroup{IVSource: "chain", LegCount: 1}, "chain ✓"},
		{GreeksGroup{IVSource: "mark", LegCount: 1}, "mark ~"},
		{GreeksGroup{IVSource: "stock", LegCount: 1}, "stock"},
		{GreeksGroup{IVSource: "mixed", LegCount: 2}, "mixed"},
		{GreeksGroup{LegCount: 0, SkippedLegCount: 1}, "✗ skipped"},
	}
	for _, c := range cases {
		if got := ivLabel(c.g); got != c.want {
			t.Errorf("ivLabel(%+v) = %q, want %q", c.g, got, c.want)
		}
	}
}

func TestRenderGreeks_ContainsHeaderAndUnits(t *testing.T) {
	r := &GreeksReport{
		GroupBy: "underlying", NetLiqUSD: 1_000_000, RiskFreeRate: 0.043,
		Groups: []GreeksGroup{{Key: "GOOGL", MVUsd: 75217, NetDelta: 180.5, DollarDelta: 1358, Gamma: 0.42, Vega: -45, Theta: -8.2, IVSource: "chain"}},
		Total:  GreeksGroup{Key: "TOTAL", NetDelta: 180.5, DollarDelta: 1358, Gamma: 0.42, Vega: -45, Theta: -8.2},
	}
	var buf bytes.Buffer
	RenderGreeks(r, &buf)
	out := buf.String()
	for _, want := range []string{"PORTFOLIO GREEKS", "Net", "Dollar", "GOOGL", "TOTAL", "chain"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q; got:\n%s", want, out)
		}
	}
}

func TestRenderGreeks_SkippedFootnote(t *testing.T) {
	r := &GreeksReport{
		GroupBy:     "underlying",
		Groups:      []GreeksGroup{{Key: "RKLB", SkippedLegCount: 1}},
		SkippedLegs: []SkippedLeg{{Symbol: "RKLB", Expiration: "20260619", Right: "C", Strike: 16, Reason: "no_iv"}},
	}
	var buf bytes.Buffer
	RenderGreeks(r, &buf)
	if !strings.Contains(buf.String(), "skipped") {
		t.Errorf("expected a skipped footnote; got:\n%s", buf.String())
	}
}

func TestGreeksReport_JSONSerializable(t *testing.T) {
	r := &GreeksReport{Groups: []GreeksGroup{{Key: "GOOGL", NetDelta: 100}}}
	if _, err := json.Marshal(r); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}
