package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/intel"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

// fakeJudgePrice 给 RegisterJudgment 固定登记价（无网络）。
type fakeJudgePrice struct{ price float64 }

func (f fakeJudgePrice) QuoteByID(_ context.Context, id string) (marketdata.Quote, error) {
	return marketdata.Quote{Ref: marketdata.AssetRef{ID: id, Class: marketdata.ClassIndex},
		Price: f.price, Basis: marketdata.BasisDelayed}, nil
}

// 端到端：CLI 路径写一轮闭环 → server HTTP /api/intel/journal 反映闭环;旧页面零回归。
func TestIntelJournalEndToEndAcceptance(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	regTime := time.Date(2026, 6, 12, 14, 30, 0, 0, intel.NY()) // first_check 已过,reconcile 未到

	j := intel.NewIntelJournal(store, fakeJudgePrice{price: 100})
	j.Now = func() time.Time { return regTime }

	// 1. 写叙事 + 两条判断(一注定 hit、一注定 miss)
	if _, err := j.WriteNarrative(ctx, "set_tone", "定调:科技领涨"); err != nil {
		t.Fatal(err)
	}
	up, err := j.RegisterJudgment(ctx, intel.JudgmentInput{AssetID: "SPX", Direction: "up",
		ThresholdPct: 0.5, Confidence: 70, ExpiryCheckpoint: "reconcile"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.RegisterJudgment(ctx, intel.JudgmentInput{AssetID: "SPX", Direction: "down",
		ThresholdPct: 0.5, Confidence: 60, ExpiryCheckpoint: "reconcile"}); err != nil {
		t.Fatal(err)
	}
	// 2. seed 到期价(reconcile≈16:30,close=101 → SPX 涨 1%)
	expAt, _ := intel.CheckpointTime("2026-06-12", "reconcile")
	if err := store.UpsertPulseBars(ctx, "SPX", []model.OHLCV{
		{Timestamp: expAt.Add(-5 * time.Minute).UTC(), Close: 101},
	}); err != nil {
		t.Fatal(err)
	}
	// 3. reconcile 在 16:35
	j.Now = func() time.Time { return time.Date(2026, 6, 12, 16, 35, 0, 0, intel.NY()) }
	if res, err := j.Reconcile(ctx); err != nil || res.Settled != 2 {
		t.Fatalf("reconcile = %+v err=%v", res, err)
	}

	// 4. 起真实 server,穿 HTTP 面读
	srv := New(Config{Addr: "127.0.0.1:0"}, store)
	srv.AttachIntel(&intel.Handlers{
		Journal: func() *intel.IntelJournal {
			jj := intel.NewIntelJournal(store, fakeJudgePrice{price: 100})
			jj.Now = func() time.Time { return time.Date(2026, 6, 12, 16, 35, 0, 0, intel.NY()) }
			return jj
		}(),
	})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/intel/journal?date=2026-06-12")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var snap intel.JournalSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Narratives) != 1 || snap.Narratives[0].Body != "定调:科技领涨" {
		t.Errorf("narratives = %+v", snap.Narratives)
	}
	if len(snap.Judgments) != 2 {
		t.Fatalf("want 2 judgments, got %d", len(snap.Judgments))
	}
	// 内联结算 + 命中率 1/2(up hit、down miss;无 void)
	var upRec *model.IntelReconciliation
	for i := range snap.Judgments {
		if snap.Judgments[i].JudgmentID == up.JudgmentID {
			upRec = snap.Judgments[i].Reconciliation
		}
	}
	if upRec == nil || upRec.Outcome != "hit" {
		t.Errorf("up judgment reconciliation = %+v", upRec)
	}
	if snap.HitRate.Hit != 1 || snap.HitRate.Miss != 1 || snap.HitRate.Rate != 0.5 {
		t.Errorf("hit_rate = %+v, want 1/1/0.5", snap.HitRate)
	}

	// 5. /intel/ 面板数据源在线 + 旧页面零回归
	for _, path := range []string{"/intel/", "/dashboard"} {
		r, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Errorf("GET %s = %d, want 200", path, r.StatusCode)
		}
	}
}
