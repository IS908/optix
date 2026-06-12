package marketdata

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/pkg/model"
)

// scriptedSource: 可注入缺席、错误与延迟的 fake。
type scriptedSource struct {
	quotes  map[string]Quote
	err     error
	barsErr error
	delay   time.Duration // BatchQuotes 人为延迟（并发 singleflight 测试用）
	calls   atomic.Int32
}

func (s *scriptedSource) Name() string { return "scripted" }
func (s *scriptedSource) BatchQuotes(_ context.Context, refs []AssetRef) (map[string]Quote, error) {
	s.calls.Add(1)
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if s.err != nil {
		return nil, s.err
	}
	out := map[string]Quote{}
	for _, r := range refs {
		if q, ok := s.quotes[r.ID]; ok {
			out[r.ID] = q
		}
	}
	return out, nil
}
func (s *scriptedSource) Bars(context.Context, AssetRef, string, time.Duration) ([]model.OHLCV, error) {
	return nil, s.barsErr
}

func newTestPulse(src Source) *PulseService {
	r := NewRouter()
	for _, c := range []AssetClass{ClassIndex, ClassFuture, ClassStock, ClassFX, ClassYield, ClassVol} {
		r.Register(c, src)
	}
	return NewPulseService(r, nil) // store=nil → sparkline 路径跳过
}

func TestSnapshot_MissingAssetDoesNotKillScreen(t *testing.T) {
	src := &scriptedSource{quotes: map[string]Quote{
		"ES": {Ref: AssetRef{ID: "ES", Class: ClassFuture}, Price: 6012, Basis: BasisDelayed},
		// 其余资产缺席
	}}
	p := newTestPulse(src)
	snap, err := p.Snapshot(context.Background(), ViewPremarket, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Assets) == 0 {
		t.Fatal("present assets must render")
	}
	if len(snap.Missing) == 0 {
		t.Fatal("absent assets must be listed in Missing")
	}
	for _, id := range snap.Missing {
		if id == "ES" {
			t.Fatal("ES is present; must not be in Missing")
		}
	}
}

func TestSnapshot_SourceTotalFailureReturnsSnapshotWithWarnings(t *testing.T) {
	src := &scriptedSource{err: errors.New("python unavailable")}
	p := newTestPulse(src)
	snap, err := p.Snapshot(context.Background(), ViewIntraday, false)
	if err != nil {
		t.Fatalf("total source failure must not error the snapshot: %v", err)
	}
	if len(snap.Assets) != 0 || len(snap.Warnings) == 0 || len(snap.Missing) == 0 {
		t.Fatalf("snap = %+v", snap)
	}
}

// findAsset：按 ID 取资产；找不到直接 Fatal（防御空 Assets 导致的虚假通过）。
func findAsset(t *testing.T, snap *PulseSnapshot, id string) PulseAsset {
	t.Helper()
	for _, a := range snap.Assets {
		if a.Ref.ID == id {
			return a
		}
	}
	t.Fatalf("asset %s not found in snapshot assets (vacuous pass guard)", id)
	return PulseAsset{}
}

func TestSnapshot_FrozenBasisAdjustment(t *testing.T) {
	src := &scriptedSource{quotes: map[string]Quote{
		"SPX": {Ref: AssetRef{ID: "SPX", Class: ClassIndex}, Price: 5993, Basis: BasisDelayed},
		"WTI": {Ref: AssetRef{ID: "WTI", Class: ClassFuture}, Price: 71.2, Basis: BasisDelayed},
		"VIX": {Ref: AssetRef{ID: "VIX", Class: ClassIndex}, Price: 14.8, Basis: BasisDelayed},
	}}
	p := newTestPulse(src)
	ctx := context.Background()

	// postclose：index 类 delayed → frozen（收盘定格）。
	post, err := p.Snapshot(ctx, ViewPostclose, false)
	if err != nil {
		t.Fatal(err)
	}
	if a := findAsset(t, post, "SPX"); a.Basis != BasisFrozen {
		t.Fatalf("postclose SPX basis = %s, want frozen", a.Basis)
	}
	// future 隔夜照常交易 → 保持 delayed，不得误升格。
	if a := findAsset(t, post, "WTI"); a.Basis != BasisDelayed {
		t.Fatalf("postclose WTI (future) basis = %s, want delayed", a.Basis)
	}

	// premarket 同样是定格视图：index 类 delayed → frozen。
	pre, err := p.Snapshot(ctx, ViewPremarket, false)
	if err != nil {
		t.Fatal(err)
	}
	if a := findAsset(t, pre, "VIX"); a.Basis != BasisFrozen {
		t.Fatalf("premarket VIX (index) basis = %s, want frozen", a.Basis)
	}
}

func TestSnapshot_CacheTTL(t *testing.T) {
	src := &scriptedSource{quotes: map[string]Quote{
		"SPX": {Ref: AssetRef{ID: "SPX", Class: ClassIndex}, Price: 1, Basis: BasisDelayed},
	}}
	p := newTestPulse(src)
	ctx := context.Background()
	_, _ = p.Snapshot(ctx, ViewIntraday, false)
	_, _ = p.Snapshot(ctx, ViewIntraday, false) // TTL 内 → 缓存命中
	if got := src.calls.Load(); got != 1 {
		t.Fatalf("source calls = %d, want 1 (second hit served from cache)", got)
	}
}

func TestSnapshot_ConcurrentSingleflight(t *testing.T) {
	src := &scriptedSource{
		delay: 30 * time.Millisecond, // 让 5 个 goroutine 真正撞上同一次构建
		quotes: map[string]Quote{
			"SPX": {Ref: AssetRef{ID: "SPX", Class: ClassIndex}, Price: 1, Basis: BasisDelayed},
		},
	}
	p := newTestPulse(src)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.Snapshot(context.Background(), ViewIntraday, false); err != nil {
				t.Errorf("concurrent Snapshot: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := src.calls.Load(); got != 1 {
		t.Fatalf("source calls = %d, want 1 (singleflight must collapse concurrent builds)", got)
	}
}

// batchableSource：同时实现 Source 与 batchBarsSource 的 fake，分别计数。
type batchableSource struct {
	scriptedSource
	batchBarsCalls atomic.Int32
	barsCalls      atomic.Int32
}

func (s *batchableSource) Bars(ctx context.Context, ref AssetRef, interval string, lookback time.Duration) ([]model.OHLCV, error) {
	s.barsCalls.Add(1)
	return s.scriptedSource.Bars(ctx, ref, interval, lookback)
}

func (s *batchableSource) BatchBars(_ context.Context, refs []AssetRef, _ string, _ time.Duration) (map[string][]model.OHLCV, error) {
	s.batchBarsCalls.Add(1)
	now := time.Now().UTC()
	out := map[string][]model.OHLCV{}
	for _, r := range refs {
		out[r.ID] = []model.OHLCV{
			{Timestamp: now.Add(-10 * time.Minute), Close: 100},
			{Timestamp: now.Add(-5 * time.Minute), Close: 101},
		}
	}
	return out, nil
}

// blockingSource：首调 BatchQuotes 阻塞直到 ctx 取消；后续调用返回好数据。
// 用于复现「首调 ctx 死亡 → singleflight waiter 拿到降级快照」的 M2 防护场景。
type blockingSource struct {
	mu    sync.Mutex
	calls int
}

func (b *blockingSource) Name() string { return "blocking" }

func (b *blockingSource) BatchQuotes(ctx context.Context, refs []AssetRef) (map[string]Quote, error) {
	b.mu.Lock()
	b.calls++
	first := b.calls == 1
	b.mu.Unlock()
	if first {
		<-ctx.Done() // 卡住直到首调 ctx 取消
		return nil, ctx.Err()
	}
	out := map[string]Quote{}
	for _, r := range refs {
		out[r.ID] = Quote{Ref: r, Label: r.ID, Price: 100, Basis: BasisDelayed}
	}
	return out, nil
}

func (b *blockingSource) Bars(context.Context, AssetRef, string, time.Duration) ([]model.OHLCV, error) {
	return nil, nil
}

func TestSnapshot_RedispatchAfterDeadCtxBuilder(t *testing.T) {
	src := &blockingSource{}
	r := NewRouter()
	for _, c := range []AssetClass{ClassIndex, ClassFuture, ClassStock, ClassFX, ClassYield, ClassVol} {
		r.Register(c, src)
	}
	svc := NewPulseService(r, nil)

	ctxA, cancelA := context.WithCancel(context.Background())
	done := make(chan *PulseSnapshot, 1)
	go func() {
		snap, _ := svc.Snapshot(ctxA, ViewIntraday, false)
		done <- snap
	}()
	time.Sleep(50 * time.Millisecond) // 让 A 进入 build 并卡在 BatchQuotes

	// B：健康 ctx，与 A 同 key 并发等待
	type result struct{ snap *PulseSnapshot }
	doneB := make(chan result, 1)
	go func() {
		snap, _ := svc.Snapshot(context.Background(), ViewIntraday, false)
		doneB <- result{snap}
	}()
	time.Sleep(50 * time.Millisecond)
	cancelA() // 杀死首调 → A 拿降级快照；B 必须重发拿到好数据

	snapA := <-done
	if len(snapA.Assets) != 0 {
		t.Fatalf("A (dead ctx) should get degraded snapshot, got %d assets", len(snapA.Assets))
	}
	resB := <-doneB
	if len(resB.snap.Assets) == 0 {
		t.Fatalf("B (healthy ctx) must get re-dispatched fresh snapshot, got degraded: missing=%v warnings=%v",
			resB.snap.Missing, resB.snap.Warnings)
	}
	if src.calls != 2 {
		t.Errorf("source called %d times, want 2 (first dead, second re-dispatch)", src.calls)
	}
}

func TestNewYFinanceRouter_CoversAllClasses(t *testing.T) {
	r := NewYFinanceRouter("")
	for _, c := range []AssetClass{ClassIndex, ClassFuture, ClassStock, ClassFX, ClassYield, ClassVol} {
		if _, ok := r.routes[c]; !ok {
			t.Errorf("class %s not routed", c)
		}
	}
}

func TestSnapshot_BatchSparkRefill(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	// 3 个有报价的资产，库为空 → 视图内所有资产都过期待补。
	src := &batchableSource{scriptedSource: scriptedSource{quotes: map[string]Quote{
		"SPX": {Ref: AssetRef{ID: "SPX", Class: ClassIndex}, Price: 5993, Basis: BasisDelayed},
		"NDX": {Ref: AssetRef{ID: "NDX", Class: ClassIndex}, Price: 21800, Basis: BasisDelayed},
		"WTI": {Ref: AssetRef{ID: "WTI", Class: ClassFuture}, Price: 71.2, Basis: BasisDelayed},
	}}}
	r := NewRouter()
	for _, c := range []AssetClass{ClassIndex, ClassFuture, ClassStock, ClassFX, ClassYield, ClassVol} {
		r.Register(c, src)
	}
	p := NewPulseService(r, store)

	snap, err := p.Snapshot(context.Background(), ViewIntraday, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := src.batchBarsCalls.Load(); got != 1 {
		t.Fatalf("BatchBars calls = %d, want exactly 1 (one subprocess per source)", got)
	}
	if got := src.barsCalls.Load(); got != 0 {
		t.Fatalf("per-asset Bars calls = %d, want 0 (batch path must be preferred)", got)
	}
	// 补进库的 bar 要真的渲染成 spark。
	a := findAsset(t, snap, "SPX")
	if len(a.Spark) != 2 || a.SparkWindow != "session" {
		t.Fatalf("SPX spark = %v window = %q, want 2-point session spark", a.Spark, a.SparkWindow)
	}
}
