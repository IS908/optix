package portfolio

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

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
