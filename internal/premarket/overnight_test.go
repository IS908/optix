package premarket

import "testing"

func TestChainConsistency(t *testing.T) {
	// 四环涨跌符号
	if same, total, note := chainConsistency([]float64{1.2, 0.8, 0.5, 0.3}); same != 4 || total != 4 || note == "" {
		t.Errorf("all-up = %d/%d %q", same, total, note)
	}
	// 台北转跌 → 断点
	if same, _, _ := chainConsistency([]float64{1.2, -0.4, 0.5, 0.3}); same == 4 {
		t.Errorf("break point should reduce same count, got %d", same)
	}
	// 全跌也算一致(同向)
	if same, total, _ := chainConsistency([]float64{-1, -0.5, -0.2, -0.1}); same != 4 || total != 4 {
		t.Errorf("all-down = %d/%d", same, total)
	}
	// 空/单环
	if _, total, _ := chainConsistency(nil); total != 0 {
		t.Error("empty chain total should be 0")
	}
}
