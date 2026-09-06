package premarket

import "testing"

func TestVixTermPremium(t *testing.T) {
	if got := vixTermPremium(20, 22); got <= 1 {
		t.Errorf("VIX3M>VIX → contango >1, got %v", got)
	}
	if got := vixTermPremium(25, 20); got >= 1 {
		t.Errorf("VIX3M<VIX → backwardation <1, got %v", got)
	}
	if got := vixTermPremium(0, 22); got != 0 {
		t.Errorf("zero VIX → 0, got %v", got)
	}
}

func TestRegime(t *testing.T) {
	// 高 P/C + backwardation → 防御；低 P/C + contango → 偏多
	if r := regimeLabel(1.3, 0.9, true); r != "防御" {
		t.Errorf("high PC + backwardation = %q, want 防御", r)
	}
	if r := regimeLabel(0.7, 1.15, true); r != "偏多" {
		t.Errorf("low PC + contango = %q, want 偏多", r)
	}
	if r := regimeLabel(1.0, 1.0, true); r != "中性" {
		t.Errorf("mid = %q, want 中性", r)
	}
}
