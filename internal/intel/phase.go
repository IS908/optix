package intel

import (
	"time"

	"github.com/IS908/optix/internal/marketdata"
)

// Phase 是市场时段四态。M2 = t 的纯函数（时段轨道）；M6/M7 将以
// Resolve(t, store) (Phase, source) 形态叠加事件抢占/regime 触发，本函数兜底。
type Phase string

const (
	PhasePremarket Phase = "premarket" // 交易日 04:00–09:30 ET
	PhaseIntraday  Phase = "intraday"  // 交易日 09:30–16:00（半日 –13:00）
	PhasePostclose Phase = "postclose" // 收盘后 4 小时（16:00–20:00；半日 13:00–17:00）
	PhaseClosed    Phase = "closed"    // 其余：隔夜、周末、整日假日、半日 17:00 后
)

// PhaseAt 返回 t 时刻的市场时段（America/New_York；DST 由 time 库处理）。
func PhaseAt(t time.Time) Phase {
	et := t.In(nyLoc)
	if !isTradingDay(et) {
		return PhaseClosed
	}
	mins := et.Hour()*60 + et.Minute()
	closeM := 16 * 60
	if ec, half := earlyCloseAt(et); half {
		closeM = ec.Hour()*60 + ec.Minute()
	}
	switch {
	case mins >= 4*60 && mins < 9*60+30:
		return PhasePremarket
	case mins >= 9*60+30 && mins < closeM:
		return PhaseIntraday
	case mins >= closeM && mins < closeM+4*60:
		return PhasePostclose
	default:
		return PhaseClosed
	}
}

// NextTransition 返回 t 之后下一次时段切换的时刻与目标时段（跨周末/假日由日历推算）。
func NextTransition(t time.Time) (time.Time, Phase) {
	et := t.In(nyLoc)
	// 14 天上限足以跨任何假日簇（最长连休 ≤ 4 天）。
	for d := 0; d < 14; d++ {
		day := et.AddDate(0, 0, d)
		for _, b := range dayBoundaries(day) {
			if b.After(et) {
				return b, PhaseAt(b)
			}
		}
	}
	return time.Time{}, PhaseClosed // 不可达（周末降级判定下每周必有交易日）
}

// dayBoundaries：day 当日的时段边界（非交易日无边界）。day 须为 ET。
func dayBoundaries(day time.Time) []time.Time {
	if !isTradingDay(day) {
		return nil
	}
	y, m, d := day.Date()
	at := func(h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, nyLoc) }
	if ec, half := earlyCloseAt(day); half {
		return []time.Time{at(4, 0), at(9, 30), ec, ec.Add(4 * time.Hour)}
	}
	return []time.Time{at(4, 0), at(9, 30), at(16, 0), at(20, 0)}
}

// ViewFor 映射时段→Pulse 视图。closed→postclose：闭市看上一时段冻结快照
// （M1 adjustBasis 已把 postclose 视图的 Index/Yield 升格 frozen）。
// event/shock 视图不经此映射，仅显式指定可达。
func ViewFor(p Phase) marketdata.View {
	switch p {
	case PhasePremarket:
		return marketdata.ViewPremarket
	case PhaseIntraday:
		return marketdata.ViewIntraday
	default:
		return marketdata.ViewPostclose
	}
}

// ValidView 校验 view 字符串（CLI 与 HTTP 共用）。
func ValidView(v marketdata.View) bool {
	for _, ok := range marketdata.ValidViews {
		if v == ok {
			return true
		}
	}
	return false
}
