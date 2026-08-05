package scanjournal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// fakeStore 内存实现（register/reconcile/stats 测试共用）。
type fakeStore struct {
	cands   []model.ScanCandidate
	recs    map[string]model.ScanReconciliation
	insertN int
}

func newFakeStore() *fakeStore { return &fakeStore{recs: map[string]model.ScanReconciliation{}} }

func (f *fakeStore) InsertScanCandidates(_ context.Context, cands []model.ScanCandidate) (int, int, error) {
	reg, skip := 0, 0
	for _, c := range cands {
		dup := false
		for _, e := range f.cands {
			if e.CandidateID == c.CandidateID {
				dup = true
				break
			}
		}
		if dup {
			skip++
			continue
		}
		f.cands = append(f.cands, c)
		reg++
	}
	f.insertN++
	return reg, skip, nil
}

func (f *fakeStore) ExpiredUnsettledScanCandidates(_ context.Context, beforeDate string) ([]model.ScanCandidate, error) {
	out := []model.ScanCandidate{}
	for _, c := range f.cands {
		if c.Expiry < beforeDate {
			if _, ok := f.recs[c.CandidateID]; !ok {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func (f *fakeStore) InsertScanReconciliation(_ context.Context, r model.ScanReconciliation) error {
	f.recs[r.CandidateID] = r
	return nil
}

func (f *fakeStore) ListScanCandidatesSince(_ context.Context, fromDate string) ([]model.ScanCandidate, error) {
	out := []model.ScanCandidate{}
	for _, c := range f.cands {
		if fromDate == "" || c.ScanDate >= fromDate {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStore) ListScanReconciliations(_ context.Context, ids []string) (map[string]model.ScanReconciliation, error) {
	out := map[string]model.ScanReconciliation{}
	for _, id := range ids {
		if r, ok := f.recs[id]; ok {
			out[id] = r
		}
	}
	return out, nil
}

func fp(v float64) *float64 { return &v }

func validInput(rank int, symbol string) CandidateInput {
	return CandidateInput{
		Rank: rank, Symbol: symbol, Expiry: "2026-08-21", DTE: 23,
		Strike: 145, Spot: 155.01, Bid: 18.90, Ask: fp(19.80), Mid: fp(19.35),
		CushionPct: 6.5, PremiumYieldPct: 13.0, AnnualizedYieldPct: 211.8, Score: 1.2,
	}
}

func nyTime(y int, m time.Month, d, hh int) time.Time {
	return time.Date(y, m, d, hh, 0, 0, 0, time.UTC) // Register 只取 NY 日期，UTC 正午在 NY 同日
}

func newTestService(store Store) *Service {
	s := NewService(store, nil)
	s.Now = func() time.Time { return nyTime(2026, 7, 29, 16) }
	return s
}

func TestRegisterHappyPathAndIdempotent(t *testing.T) {
	fs := newFakeStore()
	svc := newTestService(fs)
	p := RegisterPayload{SymbolSource: "test", Candidates: []CandidateInput{validInput(1, "NBIS"), validInput(2, "SNDK")}}
	res, err := svc.Register(context.Background(), p)
	if err != nil || res.Registered != 2 || res.Skipped != 0 || res.ScanDate != "2026-07-29" {
		t.Fatalf("register = %+v err=%v", res, err)
	}
	if fs.cands[0].CandidateID != "2026-07-29:NBIS:2026-08-21:145" || fs.cands[0].Right != "P" {
		t.Fatalf("candidate = %+v", fs.cands[0])
	}
	res, err = svc.Register(context.Background(), p)
	if err != nil || res.Registered != 0 || res.Skipped != 2 {
		t.Fatalf("re-register = %+v err=%v", res, err)
	}
}

func TestRegisterValidationRejectsWholeBatch(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*RegisterPayload)
		errSub string
	}{
		{"zero strike", func(p *RegisterPayload) { p.Candidates[1].Strike = 0 }, "strike"},
		{"zero bid", func(p *RegisterPayload) { p.Candidates[1].Bid = 0 }, "bid"},
		{"expiry not after scan_date", func(p *RegisterPayload) { p.Candidates[1].Expiry = "2026-07-29" }, "expiry"},
		{"bad expiry format", func(p *RegisterPayload) { p.Candidates[1].Expiry = "08/21/2026" }, "expiry"},
		{"rank not contiguous", func(p *RegisterPayload) { p.Candidates[1].Rank = 5 }, "rank"},
		{"empty symbol", func(p *RegisterPayload) { p.Candidates[1].Symbol = " " }, "symbol"},
		{"no candidates", func(p *RegisterPayload) { p.Candidates = nil }, "candidates"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := newFakeStore()
			svc := newTestService(fs)
			p := RegisterPayload{SymbolSource: "test", Candidates: []CandidateInput{validInput(1, "NBIS"), validInput(2, "SNDK")}}
			c.mutate(&p)
			if _, err := svc.Register(context.Background(), p); err == nil || !strings.Contains(err.Error(), c.errSub) {
				t.Fatalf("err = %v, want contains %q", err, c.errSub)
			}
			if len(fs.cands) != 0 || fs.insertN != 0 {
				t.Fatalf("store must be untouched on validation failure, got %d cands", len(fs.cands))
			}
		})
	}
}

func TestRegisterExplicitScanDateAndSymbolNormalize(t *testing.T) {
	fs := newFakeStore()
	svc := newTestService(fs)
	in := validInput(1, " nbis ")
	p := RegisterPayload{ScanDate: "2026-07-28", SymbolSource: "test", Candidates: []CandidateInput{in}}
	res, err := svc.Register(context.Background(), p)
	if err != nil || res.ScanDate != "2026-07-28" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if fs.cands[0].Symbol != "NBIS" {
		t.Fatalf("symbol not normalized: %q", fs.cands[0].Symbol)
	}
}
