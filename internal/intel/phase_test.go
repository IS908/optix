package intel

import (
	"testing"
	"time"

	"github.com/IS908/optix/internal/marketdata"
)

func TestPhaseAt(t *testing.T) {
	cases := []struct {
		name string
		t    time.Time
		want Phase
	}{
		// 常规交易日（EDT）
		{"premarket open edge 04:00", dET(2026, 6, 12, 4, 0), PhasePremarket},
		{"premarket 08:00", dET(2026, 6, 12, 8, 0), PhasePremarket},
		{"open edge 09:30", dET(2026, 6, 12, 9, 30), PhaseIntraday},
		{"intraday 13:00", dET(2026, 6, 12, 13, 0), PhaseIntraday},
		{"close edge 16:00", dET(2026, 6, 12, 16, 0), PhasePostclose},
		{"postclose 19:59", dET(2026, 6, 12, 19, 59), PhasePostclose},
		{"overnight edge 20:00", dET(2026, 6, 12, 20, 0), PhaseClosed},
		{"overnight 23:00", dET(2026, 6, 12, 23, 0), PhaseClosed},
		{"overnight 02:00", dET(2026, 6, 12, 2, 0), PhaseClosed},
		// EST（冬令时）
		{"EST intraday", dET(2026, 1, 15, 10, 0), PhaseIntraday},
		// DST 切换日本身
		{"spring-forward 2026-03-08 09:30", dET(2026, 3, 8, 9, 30), PhaseClosed}, // 周日
		{"spring-forward next day open", dET(2026, 3, 9, 9, 30), PhaseIntraday},
		{"fall-back 2026-11-01 03:00", dET(2026, 11, 1, 3, 0), PhaseClosed}, // 周日
		// 周末/假日
		{"saturday noon", dET(2026, 6, 13, 12, 0), PhaseClosed},
		{"july4 observed noon", dET(2026, 7, 3, 12, 0), PhaseClosed},
		// 半日（2026-11-27：13:00 收盘，postclose 13:00–17:00）
		{"half day intraday 12:59", dET(2026, 11, 27, 12, 59), PhaseIntraday},
		{"half day postclose 13:00", dET(2026, 11, 27, 13, 0), PhasePostclose},
		{"half day postclose 16:59", dET(2026, 11, 27, 16, 59), PhasePostclose},
		{"half day closed 17:00", dET(2026, 11, 27, 17, 0), PhaseClosed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PhaseAt(c.t); got != c.want {
				t.Errorf("PhaseAt(%s) = %s, want %s", c.t, got, c.want)
			}
		})
	}
}

func TestNextTransition(t *testing.T) {
	cases := []struct {
		name      string
		t         time.Time
		wantAt    time.Time
		wantPhase Phase
	}{
		{"intraday → 16:00 postclose", dET(2026, 6, 12, 13, 0), dET(2026, 6, 12, 16, 0), PhasePostclose},
		{"postclose → 20:00 closed", dET(2026, 6, 12, 17, 0), dET(2026, 6, 12, 20, 0), PhaseClosed},
		// 周五晚 → 跨周末 → 周一 04:00 premarket
		{"friday night spans weekend", dET(2026, 6, 12, 21, 0), dET(2026, 6, 15, 4, 0), PhasePremarket},
		// 7/2 周四晚 → 跨 7/3 观察假日 + 周末 → 7/6 周一 04:00
		{"spans july4 weekend", dET(2026, 7, 2, 21, 0), dET(2026, 7, 6, 4, 0), PhasePremarket},
		// 半日 11:00 → 13:00 postclose
		{"half day → 13:00", dET(2026, 11, 27, 11, 0), dET(2026, 11, 27, 13, 0), PhasePostclose},
		{"half day postclose → 17:00 closed", dET(2026, 11, 27, 14, 0), dET(2026, 11, 27, 17, 0), PhaseClosed},
		// DST 切换跨扫描（spring-forward 2026-03-08、fall-back 2026-11-01 均为周日）
		{"spans spring-forward", dET(2026, 3, 6, 21, 0), dET(2026, 3, 9, 4, 0), PhasePremarket},
		{"spans fall-back", dET(2026, 10, 30, 21, 0), dET(2026, 11, 2, 4, 0), PhasePremarket},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at, ph := NextTransition(c.t)
			if !at.Equal(c.wantAt) || ph != c.wantPhase {
				t.Errorf("NextTransition(%s) = (%s, %s), want (%s, %s)", c.t, at, ph, c.wantAt, c.wantPhase)
			}
		})
	}
}

// closed→postclose 是 M2 的语义修正点（M1 隔夜→premarket），显式钉死。
func TestViewFor(t *testing.T) {
	cases := map[Phase]marketdata.View{
		PhasePremarket: marketdata.ViewPremarket,
		PhaseIntraday:  marketdata.ViewIntraday,
		PhasePostclose: marketdata.ViewPostclose,
		PhaseClosed:    marketdata.ViewPostclose,
	}
	for p, want := range cases {
		if got := ViewFor(p); got != want {
			t.Errorf("ViewFor(%s) = %s, want %s", p, got, want)
		}
	}
}

func TestValidView(t *testing.T) {
	if !ValidView(marketdata.ViewShock) || ValidView(marketdata.View("bogus")) {
		t.Error("ValidView misclassifies")
	}
}
