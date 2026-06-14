// Package intel 是 Market Intel 的调度平面：市场时段状态机（M2 纯函数时段
// 轨道；M6 事件抢占、M7 regime 触发将以 DB-backed override 叠加，PhaseAt 兜底）
// 与 /api/intel/* HTTP 契约。零 IBKR、零 LLM。
package intel

import "time"

// nyLoc：America/New_York（二进制已嵌 time/tzdata —— cmd 两个 main 均 import）。
var nyLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.FixedZone("EST", -5*3600) // 极端环境兜底
	}
	return loc
}()

// NY 返回 America/New_York 时区（CLI 渲染与测试共用）。
func NY() *time.Location { return nyLoc }

type dayRule int

const (
	ruleHoliday    dayRule = iota + 1 // 整日休市
	ruleEarlyClose                    // 13:00 ET 提前收盘
)

// calendarMaxYear：表覆盖上限。超出按「仅周末」降级判定 + CalendarStale 警示。
const calendarMaxYear = 2028

// nyseCalendar：NYSE 2026-2028 假日/半日（手工维护；完整性测试钉条目数）。
// 来源：https://www.nyse.com/trade/hours-calendars（NYSE Holidays & Trading Hours）
// 规则来源：7/4 落周六→前一周五休；落周日→下周一休；圣诞/Juneteenth 同理。
// 半日：感恩节后周五、12/24（当其为交易日时）、7/3（当其为交易日时，即 7/4 落
// 周二至周五）。2028 的 1/1 落周六，NYSE 官方表明确不补休。
var nyseCalendar = map[string]dayRule{
	// 2026 整日
	"2026-01-01": ruleHoliday, "2026-01-19": ruleHoliday, "2026-02-16": ruleHoliday,
	"2026-04-03": ruleHoliday, "2026-05-25": ruleHoliday, "2026-06-19": ruleHoliday,
	"2026-07-03": ruleHoliday, "2026-09-07": ruleHoliday, "2026-11-26": ruleHoliday,
	"2026-12-25": ruleHoliday,
	// 2026 半日
	"2026-11-27": ruleEarlyClose, "2026-12-24": ruleEarlyClose,
	// 2027 整日
	"2027-01-01": ruleHoliday, "2027-01-18": ruleHoliday, "2027-02-15": ruleHoliday,
	"2027-03-26": ruleHoliday, "2027-05-31": ruleHoliday, "2027-06-18": ruleHoliday,
	"2027-07-05": ruleHoliday, "2027-09-06": ruleHoliday, "2027-11-25": ruleHoliday,
	"2027-12-24": ruleHoliday,
	// 2027 半日
	"2027-11-26": ruleEarlyClose,
	// 2028 整日（1/1 周六不补休，故 2028 年内只有 9 个整日休市）
	"2028-01-17": ruleHoliday, "2028-02-21": ruleHoliday, "2028-04-14": ruleHoliday,
	"2028-05-29": ruleHoliday, "2028-06-19": ruleHoliday, "2028-07-04": ruleHoliday,
	"2028-09-04": ruleHoliday, "2028-11-23": ruleHoliday, "2028-12-25": ruleHoliday,
	// 2028 半日
	"2028-07-03": ruleEarlyClose, "2028-11-24": ruleEarlyClose,
}

func isTradingDay(t time.Time) bool {
	et := t.In(nyLoc)
	if wd := et.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	return nyseCalendar[et.Format("2006-01-02")] != ruleHoliday
}

// earlyCloseAt：t 当日为半日时返回 (13:00 ET, true)。
func earlyCloseAt(t time.Time) (time.Time, bool) {
	et := t.In(nyLoc)
	if nyseCalendar[et.Format("2006-01-02")] != ruleEarlyClose {
		return time.Time{}, false
	}
	return time.Date(et.Year(), et.Month(), et.Day(), 13, 0, 0, 0, nyLoc), true
}

// CalendarStale：t 超出假日表覆盖年份（届时只剩周末判定，需更新表）。
func CalendarStale(t time.Time) bool { return t.In(nyLoc).Year() > calendarMaxYear }
