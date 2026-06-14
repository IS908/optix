package intel

import (
	"testing"
	"time"
)

func dET(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, NY())
}

func TestIsTradingDay(t *testing.T) {
	cases := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"normal weekday", dET(2026, 6, 12, 12, 0), true},
		{"saturday", dET(2026, 6, 13, 12, 0), false},
		{"sunday", dET(2026, 6, 14, 12, 0), false},
		{"july4 observed fri", dET(2026, 7, 3, 12, 0), false},
		{"good friday 2026", dET(2026, 4, 3, 12, 0), false},
		{"thanksgiving 2026", dET(2026, 11, 26, 12, 0), false},
		{"half day is trading day", dET(2026, 11, 27, 12, 0), true},
		{"juneteenth 2027 observed", dET(2027, 6, 18, 12, 0), false},
		{"christmas 2027 observed fri", dET(2027, 12, 24, 12, 0), false},
		{"new year 2028 not observed", dET(2028, 1, 3, 12, 0), true},
		{"independence day 2028", dET(2028, 7, 4, 12, 0), false},
		{"thanksgiving 2028", dET(2028, 11, 23, 12, 0), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTradingDay(c.t); got != c.want {
				t.Errorf("isTradingDay(%s) = %v, want %v", c.t, got, c.want)
			}
		})
	}
}

func TestEarlyClose(t *testing.T) {
	if ec, half := earlyCloseAt(dET(2026, 11, 27, 9, 0)); !half || ec.Hour() != 13 {
		t.Errorf("2026-11-27 should be 13:00 early close, got %v %v", ec, half)
	}
	if _, half := earlyCloseAt(dET(2026, 6, 12, 9, 0)); half {
		t.Error("normal day must not be early close")
	}
	// 2027-12-23：12/24 已是观察假日，前一天是全天
	if _, half := earlyCloseAt(dET(2027, 12, 23, 9, 0)); half {
		t.Error("2027-12-23 is a FULL day (12-24 observed holiday)")
	}
	if ec, half := earlyCloseAt(dET(2028, 7, 3, 9, 0)); !half || ec.Hour() != 13 {
		t.Errorf("2028-07-03 should be 13:00 early close, got %v %v", ec, half)
	}
	if ec, half := earlyCloseAt(dET(2028, 11, 24, 9, 0)); !half || ec.Hour() != 13 {
		t.Errorf("2028-11-24 should be 13:00 early close, got %v %v", ec, half)
	}
}

// 钉死表完整性：2028 的 1/1 落周六且 NYSE 不补休，故整日休市为 9 个。
func TestCalendarCompleteness(t *testing.T) {
	counts := map[int]map[dayRule]int{2026: {}, 2027: {}, 2028: {}}
	for date, rule := range nyseCalendar {
		d, err := time.Parse("2006-01-02", date)
		if err != nil {
			t.Fatalf("bad calendar key %q", date)
		}
		counts[d.Year()][rule]++
	}
	if counts[2026][ruleHoliday] != 10 || counts[2027][ruleHoliday] != 10 || counts[2028][ruleHoliday] != 9 {
		t.Errorf("holidays: 2026=%d 2027=%d 2028=%d, want 10/10/9",
			counts[2026][ruleHoliday], counts[2027][ruleHoliday], counts[2028][ruleHoliday])
	}
	if counts[2026][ruleEarlyClose] != 2 || counts[2027][ruleEarlyClose] != 1 || counts[2028][ruleEarlyClose] != 2 {
		t.Errorf("half days: 2026=%d 2027=%d 2028=%d, want 2/1/2",
			counts[2026][ruleEarlyClose], counts[2027][ruleEarlyClose], counts[2028][ruleEarlyClose])
	}
}

func TestCalendarStale(t *testing.T) {
	if CalendarStale(dET(2027, 12, 31, 12, 0)) {
		t.Error("2027 covered → not stale")
	}
	if CalendarStale(dET(2028, 12, 29, 12, 0)) {
		t.Error("2028 covered → not stale")
	}
	if !CalendarStale(dET(2029, 1, 2, 12, 0)) {
		t.Error("2029 beyond table → stale")
	}
	// 表滚出后按「仅周末」降级判定；2029-01-01 真实假日会被视为交易日。
	if !isTradingDay(dET(2029, 1, 1, 12, 0)) {
		t.Error("2029 beyond table must degrade to weekend-only judgment")
	}
}
