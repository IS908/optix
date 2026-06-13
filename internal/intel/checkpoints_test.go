package intel

import (
	"testing"
	"time"
)

// dET 在 checkpoints_test 复用（与 calendar_test 同包；calendar_test 已定义 dET，勿重复定义）。
// 若编译报 dET redeclared，删本注释下的占位并直接用同包已有的 dET。

func TestCheckpointStatus(t *testing.T) {
	// 交易日 11:00 ET：script(8:00)/first_check(10:30) 已过 → 未写则 due；
	// set_tone(15:00)/reconcile(16:30) 未到 → pending。
	now := dET(2026, 6, 12, 11, 0)
	written := map[string]bool{"script": true} // 只写了剧本
	states := CheckpointStatus(now, written)
	got := map[string]string{}
	for _, s := range states {
		got[s.Kind] = s.Status
	}
	want := map[string]string{
		"script": "written", "first_check": "due",
		"set_tone": "pending", "reconcile": "pending",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("checkpoint %s = %s, want %s", k, got[k], v)
		}
	}
	if len(states) != 4 {
		t.Fatalf("want 4 checkpoints, got %d", len(states))
	}
}

func TestCheckpointStatusAllWritten(t *testing.T) {
	now := dET(2026, 6, 12, 17, 0)
	written := map[string]bool{"script": true, "first_check": true, "set_tone": true, "reconcile": true}
	for _, s := range CheckpointStatus(now, written) {
		if s.Status != "written" {
			t.Errorf("%s = %s, want written", s.Kind, s.Status)
		}
	}
}

func TestCheckpointTime(t *testing.T) {
	at, ok := CheckpointTime("2026-06-12", "reconcile")
	if !ok || at.Hour() != 16 || at.Minute() != 30 || at.Location() != nyLoc {
		t.Errorf("reconcile time = %v ok=%v", at, ok)
	}
	if _, ok := CheckpointTime("2026-06-12", "interrupt"); ok {
		t.Error("interrupt is not a scheduled checkpoint")
	}
	if _, ok := CheckpointTime("bad-date", "reconcile"); ok {
		t.Error("bad date must return ok=false")
	}
}

func TestTradingDate(t *testing.T) {
	// 19:00 UTC = 15:00 EDT 同日；02:00 UTC = 22:00 EDT 前一日
	if d := TradingDate(time.Date(2026, 6, 12, 19, 0, 0, 0, time.UTC)); d != "2026-06-12" {
		t.Errorf("TradingDate = %s, want 2026-06-12", d)
	}
}
