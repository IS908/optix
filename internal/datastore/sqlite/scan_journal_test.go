package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func f64(v float64) *float64 { return &v }

func sampleCandidate(id, scanDate, symbol, expiry string, strike float64) model.ScanCandidate {
	return model.ScanCandidate{
		CandidateID: id, ScanDate: scanDate, Rank: 1, Symbol: symbol, Right: "P",
		Expiry: expiry, DTE: 9, Strike: strike, Spot: strike * 1.08, Bid: 2.5,
		Ask: f64(2.7), Mid: f64(2.6), CushionPct: 7.4, PremiumYieldPct: 1.7,
		AnnualizedYieldPct: 70.0, Score: 0.9, SymbolSource: "test",
		CreatedAt: time.Date(2026, 7, 29, 14, 0, 0, 0, time.UTC),
	}
}

func TestScanCandidatesInsertIdempotentAndRoundTrip(t *testing.T) {
	s, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	c1 := sampleCandidate("2026-07-29:NBIS:2026-08-21:145", "2026-07-29", "NBIS", "2026-08-21", 145)
	c2 := sampleCandidate("2026-07-29:SNDK:2026-08-07:890", "2026-07-29", "SNDK", "2026-08-07", 890)
	reg, skip, err := s.InsertScanCandidates(ctx, []model.ScanCandidate{c1, c2})
	if err != nil || reg != 2 || skip != 0 {
		t.Fatalf("insert = (%d,%d,%v), want (2,0,nil)", reg, skip, err)
	}
	// 幂等：重插同锚点全部 skipped
	reg, skip, err = s.InsertScanCandidates(ctx, []model.ScanCandidate{c1, c2})
	if err != nil || reg != 0 || skip != 2 {
		t.Fatalf("re-insert = (%d,%d,%v), want (0,2,nil)", reg, skip, err)
	}
	// 往返：字段保真（含可空指针）
	got, err := s.ListScanCandidatesSince(ctx, "")
	if err != nil || len(got) != 2 {
		t.Fatalf("list = %d,%v", len(got), err)
	}
	byID := map[string]model.ScanCandidate{}
	for _, c := range got {
		byID[c.CandidateID] = c
	}
	rt := byID[c1.CandidateID]
	if rt.Symbol != "NBIS" || rt.Strike != 145 || rt.Right != "P" || *rt.Mid != 2.6 || rt.IV != nil {
		t.Fatalf("round-trip mismatch: %+v", rt)
	}
}

func TestExpiredUnsettledAndReconciliationFlow(t *testing.T) {
	s, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	old := sampleCandidate("2026-07-01:AAPL:2026-07-18:200", "2026-07-01", "AAPL", "2026-07-18", 200)
	fut := sampleCandidate("2026-07-29:MSFT:2026-09-18:500", "2026-07-29", "MSFT", "2026-09-18", 500)
	if _, _, err := s.InsertScanCandidates(ctx, []model.ScanCandidate{old, fut}); err != nil {
		t.Fatal(err)
	}
	exp, err := s.ExpiredUnsettledScanCandidates(ctx, "2026-07-29")
	if err != nil || len(exp) != 1 || exp[0].CandidateID != old.CandidateID {
		t.Fatalf("expired = %+v err=%v, want only AAPL", exp, err)
	}
	rec := model.ScanReconciliation{
		CandidateID: old.CandidateID, ExpiryClose: 210, Outcome: "hit",
		RealizedPnL: 2.5, Touched: false, MaxBreachPct: 0,
		ExpiryBasis: "delayed", SettledAt: time.Now().UTC(),
	}
	if err := s.InsertScanReconciliation(ctx, rec); err != nil {
		t.Fatal(err)
	}
	// 结算后不再出现在 expired-unsettled
	exp, _ = s.ExpiredUnsettledScanCandidates(ctx, "2026-07-29")
	if len(exp) != 0 {
		t.Fatalf("after settle, expired = %+v, want empty", exp)
	}
	recs, err := s.ListScanReconciliations(ctx, []string{old.CandidateID, fut.CandidateID})
	if err != nil || len(recs) != 1 || recs[old.CandidateID].Outcome != "hit" || recs[old.CandidateID].Touched != false {
		t.Fatalf("recs = %+v err=%v", recs, err)
	}
}
