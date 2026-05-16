package journal

import (
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func mkExec(id, symbol, side string, qty, price float64, ts time.Time) model.Execution {
	return model.Execution{
		ExecID: id, Time: ts, Account: "DU1", Symbol: symbol,
		SecType: "STK", Side: side, Shares: qty, Price: price, AvgPrice: price,
	}
}

func TestMatcherSimpleLongOpenClose(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	t1 := t0.Add(48 * time.Hour)
	rts := MatchRoundTrips([]model.Execution{
		mkExec("E1", "AAPL", "BOT", 100, 150, t0),
		mkExec("E2", "AAPL", "SLD", 100, 160, t1),
	}, t1.Add(time.Hour))
	if len(rts) != 1 {
		t.Fatalf("len=%d, want 1: %+v", len(rts), rts)
	}
	r := rts[0]
	if r.Direction != "LONG" || r.Status != "closed" || r.RealizedPnL != 1000 ||
		r.OpenQty != 100 || r.CloseQty != 100 || r.HoldingDays != 2.0 {
		t.Errorf("rt = %+v", r)
	}
}

func TestMatcherShortOpenClose(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)
	rts := MatchRoundTrips([]model.Execution{
		mkExec("E1", "TSLA", "SLD", 50, 200, t0),
		mkExec("E2", "TSLA", "BOT", 50, 180, t1),
	}, t1.Add(time.Hour))
	if len(rts) != 1 || rts[0].Direction != "SHORT" || rts[0].Status != "closed" || rts[0].RealizedPnL != 1000 {
		t.Fatalf("rts = %+v", rts)
	}
}

func TestMatcherScalingOut(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	rts := MatchRoundTrips([]model.Execution{
		mkExec("E1", "AAPL", "BOT", 100, 100, t0),
		mkExec("E2", "AAPL", "SLD", 30, 110, t0.Add(24*time.Hour)),
		mkExec("E3", "AAPL", "SLD", 70, 120, t0.Add(48*time.Hour)),
	}, t0.Add(72*time.Hour))
	if len(rts) != 2 {
		t.Fatalf("len=%d", len(rts))
	}
	var total float64
	for _, r := range rts {
		total += r.RealizedPnL
	}
	if total != 1700 {
		t.Errorf("total PnL = %v, want 1700", total)
	}
}

func TestMatcherScalingIn(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	rts := MatchRoundTrips([]model.Execution{
		mkExec("E1", "AAPL", "BOT", 30, 100, t0),
		mkExec("E2", "AAPL", "BOT", 70, 110, t0.Add(24*time.Hour)),
		mkExec("E3", "AAPL", "SLD", 100, 120, t0.Add(48*time.Hour)),
	}, t0.Add(72*time.Hour))
	if len(rts) != 2 {
		t.Fatalf("len=%d", len(rts))
	}
	var total float64
	for _, r := range rts {
		total += r.RealizedPnL
	}
	if total != 1300 {
		t.Errorf("total PnL = %v, want 1300", total)
	}
}

func TestMatcherStillOpen(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	rts := MatchRoundTrips([]model.Execution{
		mkExec("E1", "AAPL", "BOT", 100, 150, t0),
	}, t0.Add(72*time.Hour))
	if len(rts) != 1 || rts[0].Status != "open" || rts[0].CloseQty != 0 {
		t.Fatalf("rts = %+v", rts)
	}
}

func TestMatcherEmptyInput(t *testing.T) {
	if rts := MatchRoundTrips(nil, time.Now()); len(rts) != 0 {
		t.Errorf("expected empty, got %+v", rts)
	}
}

func TestMatcherOptionExpiry(t *testing.T) {
	openTime := time.Date(2026, 4, 1, 14, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	rts := MatchRoundTrips([]model.Execution{{
		ExecID: "E1", Time: openTime, Account: "DU1", Symbol: "AAPL",
		SecType: "OPT", Expiration: "20260418", Strike: 200, Right: "C",
		Side: "BOT", Shares: 1, Price: 5.0, AvgPrice: 5.0,
	}}, asOf)
	if len(rts) != 1 || rts[0].Status != "expired" || rts[0].RealizedPnL != -500.0 || rts[0].Multiplier != 100 {
		t.Fatalf("rts = %+v", rts)
	}
}

func TestMatcherOptionStillOpenBeforeExpiry(t *testing.T) {
	openTime := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	rts := MatchRoundTrips([]model.Execution{{
		ExecID: "E1", Time: openTime, Account: "DU1", Symbol: "AAPL",
		SecType: "OPT", Expiration: "20260620", Strike: 200, Right: "C",
		Side: "BOT", Shares: 1, Price: 5.0, AvgPrice: 5.0,
	}}, asOf)
	if len(rts) != 1 || rts[0].Status != "open" {
		t.Fatalf("rts = %+v", rts)
	}
}
