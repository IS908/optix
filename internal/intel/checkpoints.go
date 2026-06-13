package intel

import "time"

// Checkpoint 是一个每日检查点定义（代码默认，不上配置 —— 像 NYSE 日历）。
type Checkpoint struct {
	Kind   string // script|first_check|set_tone|reconcile
	Label  string // 剧本|首验|定调|对账
	HourET int
	MinET  int
}

// DailyCheckpoints：四个每日检查点（美东时间）。interrupt(突发) 不在日程，ad-hoc 随时写。
var DailyCheckpoints = []Checkpoint{
	{"script", "剧本", 8, 0},
	{"first_check", "首验", 10, 30},
	{"set_tone", "定调", 15, 0},
	{"reconcile", "对账", 16, 30},
}

// validCheckpointKinds 含 interrupt（写入校验用；interrupt 可写但不在日程）。
var validCheckpointKinds = map[string]bool{
	"script": true, "first_check": true, "set_tone": true, "reconcile": true, "interrupt": true,
}

// IsCheckpointKind 校验 narrative --kind / 写入接受的检查点种类。
func IsCheckpointKind(kind string) bool { return validCheckpointKinds[kind] }

// CheckpointState 是某检查点当下的状态。
type CheckpointState struct {
	Kind   string    `json:"kind"`
	Label  string    `json:"label"`
	DueAt  time.Time `json:"due_at"`
	Status string    `json:"status"` // written|due|pending
}

// TradingDate 返回 t 的美东交易日（YYYY-MM-DD）。
func TradingDate(t time.Time) string { return t.In(nyLoc).Format("2006-01-02") }

// CheckpointTime 返回某日程检查点在 tradingDate 当日的 ET 时刻；interrupt/未知/坏日期 → ok=false。
func CheckpointTime(tradingDate, kind string) (time.Time, bool) {
	for _, c := range DailyCheckpoints {
		if c.Kind == kind {
			d, err := time.ParseInLocation("2006-01-02", tradingDate, nyLoc)
			if err != nil {
				return time.Time{}, false
			}
			return time.Date(d.Year(), d.Month(), d.Day(), c.HourET, c.MinET, 0, 0, nyLoc), true
		}
	}
	return time.Time{}, false
}

// CheckpointStatus 对四个日程检查点算状态：written(已写) / due(now≥due_at 且未写) / pending(now<due_at)。
// 非交易日：调用方传 written 全 false 时全 pending（不催）—— due 仅当 now≥due_at；
// 但非交易日语义由 status 端点的 is_trading_day 字段表达，这里仍按时钟算（简单）。
func CheckpointStatus(now time.Time, written map[string]bool) []CheckpointState {
	et := now.In(nyLoc)
	date := et.Format("2006-01-02")
	out := make([]CheckpointState, 0, len(DailyCheckpoints))
	for _, c := range DailyCheckpoints {
		dueAt, _ := CheckpointTime(date, c.Kind)
		status := "pending"
		switch {
		case written[c.Kind]:
			status = "written"
		case !et.Before(dueAt):
			status = "due"
		}
		out = append(out, CheckpointState{Kind: c.Kind, Label: c.Label, DueAt: dueAt, Status: status})
	}
	return out
}
