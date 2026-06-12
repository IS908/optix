package cli

import (
	"testing"
	"time"

	"github.com/IS908/optix/internal/marketdata"
)

func TestNewPulseCmdFlags(t *testing.T) {
	cmd := newPulseCmd()
	if got, _ := cmd.Flags().GetString("view"); got != "" {
		t.Errorf("default --view = %q, want \"\" (auto-infer)", got)
	}
	for _, name := range []string{"format", "with-sparkline", "strict"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not defined", name)
		}
	}
}

func TestInferView_ClockMapping(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	cases := []struct {
		name string
		now  time.Time
		want marketdata.View
	}{
		// EDT 夏令时（2026-06-01 为 EDT）
		{"premarket 8am", time.Date(2026, 6, 1, 8, 0, 0, 0, ny), marketdata.ViewPremarket},
		{"open 9:30", time.Date(2026, 6, 1, 9, 30, 0, 0, ny), marketdata.ViewIntraday},
		{"intraday 14:00", time.Date(2026, 6, 1, 14, 0, 0, 0, ny), marketdata.ViewIntraday},
		{"close 16:00", time.Date(2026, 6, 1, 16, 0, 0, 0, ny), marketdata.ViewPostclose},
		{"evening 19:59", time.Date(2026, 6, 1, 19, 59, 0, 0, ny), marketdata.ViewPostclose},
		{"overnight 23:00 → premarket", time.Date(2026, 6, 1, 23, 0, 0, 0, ny), marketdata.ViewPremarket},
		{"overnight 02:00 → premarket", time.Date(2026, 6, 2, 2, 0, 0, 0, ny), marketdata.ViewPremarket},
		// EST 冬令时（2026-01-15 为 EST）—— 锁定 DST 两侧行为一致
		{"EST premarket 8am", time.Date(2026, 1, 15, 8, 0, 0, 0, ny), marketdata.ViewPremarket},
		{"EST intraday 10:00", time.Date(2026, 1, 15, 10, 0, 0, 0, ny), marketdata.ViewIntraday},
		// DST 切换日验证（spring-forward 2026-03-08，fall-back 2026-11-01）
		{"DST spring-forward 9:30 intraday", time.Date(2026, 3, 8, 9, 30, 0, 0, ny), marketdata.ViewIntraday},
		{"DST fall-back 03:00 premarket", time.Date(2026, 11, 1, 3, 0, 0, 0, ny), marketdata.ViewPremarket},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inferView(c.now); got != c.want {
				t.Errorf("inferView(%v) = %s, want %s", c.now, got, c.want)
			}
		})
	}
}
