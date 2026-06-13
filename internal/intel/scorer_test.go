package intel

import (
	"math"
	"testing"
)

func TestScore(t *testing.T) {
	cases := []struct {
		name            string
		dir             string
		th, reg, exp    float64
		wantOutcome     string
		wantDeltaApprox float64
	}{
		{"up hit", "up", 0.5, 100, 101, "hit", 1.0},
		{"up miss below threshold", "up", 0.5, 100, 100.2, "miss", 0.2},
		{"up zero-threshold delta-zero is hit", "up", 0, 100, 100, "hit", 0},
		{"down hit", "down", 0.5, 100, 99, "hit", -1.0},
		{"down miss", "down", 0.5, 100, 100.2, "miss", 0.2},
		{"flat hit within band", "flat", 0.5, 100, 100.3, "hit", 0.3},
		{"flat miss outside band", "flat", 0.5, 100, 101, "miss", 1.0},
		// exact-threshold boundaries（>= / <= 钉死，防未来改成严格不等号回归）
		{"up hit at exact threshold", "up", 0.5, 100, 100.5, "hit", 0.5},
		{"down hit at exact -threshold", "down", 0.5, 100, 99.5, "hit", -0.5},
		{"flat hit at exact band edge (neg)", "flat", 0.5, 100, 99.5, "hit", -0.5},
		{"void zero registered", "up", 0, 0, 100, "void", 0},
		{"void negative expiry", "up", 0, 100, -1, "void", 0},
		{"unknown direction void", "sideways", 0, 100, 101, "void", 1.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, delta := Score(c.dir, c.th, c.reg, c.exp)
			if out != c.wantOutcome {
				t.Errorf("outcome = %s, want %s", out, c.wantOutcome)
			}
			if math.Abs(delta-c.wantDeltaApprox) > 1e-9 {
				t.Errorf("delta = %v, want ~%v", delta, c.wantDeltaApprox)
			}
		})
	}
}
