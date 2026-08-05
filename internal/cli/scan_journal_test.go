package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestScanJournalRegisterEndToEnd 用 go run 起真实 CLI：临时 DB + stdin payload。
// 快且无网络（register 不取行情）。
func TestScanJournalRegisterEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test")
	}
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	payload := map[string]any{
		"scan_date": "2026-07-28", "symbol_source": "test",
		"candidates": []map[string]any{{
			"rank": 1, "symbol": "NBIS", "expiry": "2026-08-21", "dte": 23,
			"strike": 145.0, "spot": 155.01, "bid": 18.9,
			"cushion_pct": 6.5, "premium_yield_pct": 13.0,
			"annualized_yield_pct": 211.8, "score": 1.2,
		}},
	}
	raw, _ := json.Marshal(payload)
	run := func() (string, error) {
		cmd := exec.Command("go", "run", "../../cmd/optix-cli", "--db", db, "scan-journal", "register")
		cmd.Stdin = bytes.NewReader(raw)
		cmd.Env = os.Environ()
		out, err := cmd.Output()
		return string(out), err
	}
	out, err := run()
	if err != nil {
		t.Fatalf("register run: %v (%s)", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("parse output %q: %v", out, err)
	}
	if res["registered"].(float64) != 1 || res["scan_date"] != "2026-07-28" {
		t.Fatalf("res = %v", res)
	}
	out, err = run() // 幂等
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	_ = json.Unmarshal([]byte(out), &res)
	if res["skipped"].(float64) != 1 {
		t.Fatalf("re-run res = %v", res)
	}
}
