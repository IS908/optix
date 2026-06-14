package intel

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

// fakePrice 实现 PriceSource，按 assetID 返回固定价。
type fakePrice struct {
	price float64
	basis string
	err   error
}

func (f fakePrice) QuoteByID(_ context.Context, id string) (marketdata.Quote, error) {
	if f.err != nil {
		return marketdata.Quote{}, f.err
	}
	return marketdata.Quote{Ref: marketdata.AssetRef{ID: id, Class: marketdata.ClassIndex},
		Price: f.price, Basis: marketdata.Basis(f.basis)}, nil
}

func newJournal(t *testing.T, price PriceSource, now time.Time) (*IntelJournal, *sqlite.Store) {
	t.Helper()
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	j := NewIntelJournal(s, price)
	j.Now = func() time.Time { return now }
	return j, s
}

func TestWriteNarrativeAndRead(t *testing.T) {
	now := dET(2026, 6, 12, 8, 0)
	j, _ := newJournal(t, fakePrice{price: 100, basis: "delayed"}, now)
	ctx := context.Background()
	if _, err := j.WriteNarrative(ctx, "script", "今日看涨科技"); err != nil {
		t.Fatal(err)
	}
	if _, err := j.WriteNarrative(ctx, "bogus", "x"); err == nil {
		t.Error("invalid kind must error")
	}
	snap, err := j.ReadJournal(ctx, TradingDate(now))
	if err != nil || len(snap.Narratives) != 1 || snap.Narratives[0].Body != "今日看涨科技" {
		t.Fatalf("read = %+v err=%v", snap, err)
	}
}

func TestRegisterJudgmentCapturesPrice(t *testing.T) {
	now := dET(2026, 6, 12, 10, 30) // first_check
	j, _ := newJournal(t, fakePrice{price: 4200, basis: "delayed"}, now)
	ctx := context.Background()
	jd, err := j.RegisterJudgment(ctx, JudgmentInput{
		AssetID: "SPX", Direction: "up", ThresholdPct: 0.5, Confidence: 75, ExpiryCheckpoint: "reconcile",
	})
	if err != nil {
		t.Fatal(err)
	}
	if jd.RegisteredPrice != 4200 || jd.RegisteredBasis != "delayed" {
		t.Errorf("price not captured: %+v", jd)
	}
	// expiry_at = 当日 reconcile(16:30 ET)
	wantExp, _ := CheckpointTime(TradingDate(now), "reconcile")
	if !jd.ExpiryAt.Equal(wantExp) {
		t.Errorf("expiry_at = %v, want %v", jd.ExpiryAt, wantExp)
	}
}

// 判断登记归属到「登记时最近已到期的检查点」。15:30 → set_tone（15:00 已过、16:30 未到）。
func TestRegisterJudgmentCheckpointAttribution(t *testing.T) {
	now := dET(2026, 6, 12, 15, 30) // set_tone(15:00) 已过、reconcile(16:30) 未到
	j, _ := newJournal(t, fakePrice{price: 4200, basis: "delayed"}, now)
	jd, err := j.RegisterJudgment(context.Background(), JudgmentInput{
		AssetID: "SPX", Direction: "up", Confidence: 60, ExpiryCheckpoint: "reconcile",
	})
	if err != nil {
		t.Fatal(err)
	}
	if jd.Checkpoint != "set_tone" {
		t.Errorf("checkpoint attribution = %q, want set_tone", jd.Checkpoint)
	}
}

func TestRegisterJudgmentValidation(t *testing.T) {
	now := dET(2026, 6, 12, 16, 30) // reconcile：无更晚检查点
	j, _ := newJournal(t, fakePrice{price: 4200, basis: "delayed"}, now)
	ctx := context.Background()
	// 到期检查点不晚于 now → 拒绝
	if _, err := j.RegisterJudgment(ctx, JudgmentInput{AssetID: "SPX", Direction: "up",
		Confidence: 50, ExpiryCheckpoint: "reconcile"}); err == nil {
		t.Error("expiry not in the future must be rejected")
	}
	// 非法方向
	now2 := dET(2026, 6, 12, 10, 30)
	j2, _ := newJournal(t, fakePrice{price: 4200, basis: "delayed"}, now2)
	if _, err := j2.RegisterJudgment(ctx, JudgmentInput{AssetID: "SPX", Direction: "sideways",
		Confidence: 50, ExpiryCheckpoint: "reconcile"}); err == nil {
		t.Error("invalid direction must be rejected")
	}
}

func TestRegisterJudgmentPriceUnavailable(t *testing.T) {
	now := dET(2026, 6, 12, 10, 30)
	j, _ := newJournal(t, fakePrice{err: errPriceDown}, now)
	if _, err := j.RegisterJudgment(context.Background(), JudgmentInput{AssetID: "SPX",
		Direction: "up", Confidence: 50, ExpiryCheckpoint: "reconcile"}); err == nil {
		t.Error("price-unavailable judgment must be rejected (cannot reconcile)")
	}
}

var errPriceDown = &priceErr{}

type priceErr struct{}

func (*priceErr) Error() string { return "price down" }

func TestReconcileSettlesAndHitRate(t *testing.T) {
	regTime := dET(2026, 6, 12, 10, 30)
	j, store := newJournal(t, fakePrice{price: 100, basis: "realtime"}, regTime)
	ctx := context.Background()
	// 登记两条判断（expiry reconcile=16:30 当日）
	up, _ := j.RegisterJudgment(ctx, JudgmentInput{AssetID: "SPX", Direction: "up",
		ThresholdPct: 0.5, Confidence: 70, ExpiryCheckpoint: "reconcile"})
	down, _ := j.RegisterJudgment(ctx, JudgmentInput{AssetID: "SPX", Direction: "down",
		ThresholdPct: 0.5, Confidence: 60, ExpiryCheckpoint: "reconcile"})
	// seed 到期价（reconcile 时刻附近的 bar，close=101 → 涨 1%）
	expAt, _ := CheckpointTime(TradingDate(regTime), "reconcile")
	_ = store.UpsertPulseBars(ctx, "SPX", []model.OHLCV{
		{Timestamp: expAt.Add(-5 * time.Minute).UTC(), Close: 101},
	})
	// reconcile 在 16:35 跑
	j.Now = func() time.Time { return dET(2026, 6, 12, 16, 35) }
	res, err := j.Reconcile(ctx)
	if err != nil || res.Settled != 2 {
		t.Fatalf("reconcile = %+v err=%v", res, err)
	}
	snap, _ := j.ReadJournal(ctx, TradingDate(regTime))
	// up(涨1%≥0.5) → hit；down(涨1% 不满足跌) → miss；命中率 1/2
	byID := map[string]*model.IntelReconciliation{}
	for i := range snap.Judgments {
		byID[snap.Judgments[i].JudgmentID] = snap.Judgments[i].Reconciliation
	}
	if byID[up.JudgmentID] == nil || byID[up.JudgmentID].Outcome != "hit" {
		t.Errorf("up should be hit: %+v", byID[up.JudgmentID])
	}
	if byID[up.JudgmentID].ExpiryBasis != string(marketdata.BasisFrozen) {
		t.Errorf("expiry_basis = %q, want %q for cached pulse bar", byID[up.JudgmentID].ExpiryBasis, marketdata.BasisFrozen)
	}
	if byID[down.JudgmentID] == nil || byID[down.JudgmentID].Outcome != "miss" {
		t.Errorf("down should be miss: %+v", byID[down.JudgmentID])
	}
	if snap.HitRate.Hit != 1 || snap.HitRate.Miss != 1 || snap.HitRate.Rate != 0.5 {
		t.Errorf("hit_rate = %+v, want 1/1/0.5", snap.HitRate)
	}
	if snap.HitRate.Window != TradingDate(regTime) {
		t.Errorf("hit_rate.window = %q, want queried trading date %q", snap.HitRate.Window, TradingDate(regTime))
	}
	// 幂等：再跑 reconcile 不重复结算
	res2, _ := j.Reconcile(ctx)
	if res2.Settled != 0 {
		t.Errorf("re-reconcile should settle 0, got %d", res2.Settled)
	}
}

func TestReconcileVoidOnMissingPrice(t *testing.T) {
	regTime := dET(2026, 6, 12, 10, 30)
	j, _ := newJournal(t, fakePrice{price: 100, basis: "delayed"}, regTime)
	ctx := context.Background()
	_, _ = j.RegisterJudgment(ctx, JudgmentInput{AssetID: "SPX", Direction: "up",
		Confidence: 70, ExpiryCheckpoint: "reconcile"})
	// 不 seed pulse_bars → 到期取不到价 → void
	j.Now = func() time.Time { return dET(2026, 6, 12, 16, 35) }
	res, _ := j.Reconcile(ctx)
	if res.Settled != 1 {
		t.Fatalf("settled = %d, want 1 (void counts as settled)", res.Settled)
	}
	snap, _ := j.ReadJournal(ctx, TradingDate(regTime))
	if snap.Judgments[0].Reconciliation.Outcome != "void" {
		t.Errorf("want void, got %s", snap.Judgments[0].Reconciliation.Outcome)
	}
	// void 不计命中率
	if snap.HitRate.Hit != 0 || snap.HitRate.Miss != 0 {
		t.Errorf("void must not count: %+v", snap.HitRate)
	}
}
