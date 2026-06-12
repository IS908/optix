package marketdata

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/pkg/model"
)

type View string

const (
	ViewPremarket View = "premarket"
	ViewIntraday  View = "intraday"
	ViewPostclose View = "postclose"
	ViewEvent     View = "event"
	ViewShock     View = "shock"
)

// ValidViews 供 CLI 校验。
var ValidViews = []View{ViewPremarket, ViewIntraday, ViewPostclose, ViewEvent, ViewShock}

// viewCompositions：代码默认（M1 不上 yaml —— #102/#103 教训）。
var viewCompositions = map[View][]AssetRef{
	ViewPremarket: {
		{ID: "ES", Class: ClassFuture}, {ID: "NQ", Class: ClassFuture},
		{ID: "YM", Class: ClassFuture}, {ID: "RTY_F", Class: ClassFuture},
		{ID: "VIX", Class: ClassIndex}, {ID: "US10Y", Class: ClassYield},
		{ID: "DXY", Class: ClassFX}, {ID: "WTI", Class: ClassFuture},
		{ID: "SOX_PROXY", Class: ClassStock},
	},
	ViewIntraday: {
		{ID: "SPX", Class: ClassIndex}, {ID: "NDX", Class: ClassIndex},
		{ID: "DJI", Class: ClassIndex}, {ID: "SOX", Class: ClassIndex},
		{ID: "RTY", Class: ClassIndex}, {ID: "VIX", Class: ClassIndex},
		{ID: "US10Y", Class: ClassYield}, {ID: "DXY", Class: ClassFX},
		{ID: "WTI", Class: ClassFuture},
	},
	ViewPostclose: {
		{ID: "SPX", Class: ClassIndex}, {ID: "NDX", Class: ClassIndex},
		{ID: "DJI", Class: ClassIndex}, {ID: "SOX", Class: ClassIndex},
		{ID: "RTY", Class: ClassIndex}, {ID: "VIX", Class: ClassIndex},
		{ID: "US10Y", Class: ClassYield}, {ID: "DXY", Class: ClassFX},
		{ID: "WTI", Class: ClassFuture},
	},
	ViewEvent: {
		{ID: "SPX", Class: ClassIndex}, {ID: "NDX", Class: ClassIndex},
		{ID: "DJI", Class: ClassIndex}, {ID: "SOX", Class: ClassIndex},
		{ID: "VIX", Class: ClassIndex}, {ID: "US2Y", Class: ClassFuture},
		{ID: "US10Y", Class: ClassYield}, {ID: "DXY", Class: ClassFX},
		{ID: "GOLD", Class: ClassFuture},
	},
	ViewShock: {
		{ID: "SPX", Class: ClassIndex}, {ID: "NDX", Class: ClassIndex},
		{ID: "VIX", Class: ClassIndex}, {ID: "WTI", Class: ClassFuture},
		{ID: "GOLD", Class: ClassFuture}, {ID: "US10Y", Class: ClassYield},
		{ID: "DXY", Class: ClassFX}, {ID: "RTY", Class: ClassIndex},
		{ID: "OVX", Class: ClassVol}, {ID: "VIX3M", Class: ClassVol},
	},
}

// PulseAsset = Quote + 可选 sparkline。
type PulseAsset struct {
	Quote
	Spark       []float64 // 5m 收盘序列（withSpark 时）
	SparkWindow string    // "overnight" | "session"
}

type PulseSnapshot struct {
	SnapshotAt time.Time
	View       View
	Assets     []PulseAsset
	Missing    []string
	Warnings   []string
}

// batchBarsSource is the optional bulk-bars capability. PulseService prefers
// it (one subprocess per source for all stale assets); sources without it
// fall back to per-asset Bars.
type batchBarsSource interface {
	BatchBars(ctx context.Context, refs []AssetRef, interval string, lookback time.Duration) (map[string][]model.OHLCV, error)
}

// sparkStaleAfter: yahoo 5m bars run ~15min delayed, so a just-refilled
// asset's newest bar is already 15-25min old — a threshold below the feed
// delay would refetch on every cache miss. 30min ≈ delay + one TTL window.
const sparkStaleAfter = 30 * time.Minute

type PulseService struct {
	router *Router
	store  *sqlite.Store // nil 容忍（测试/无库场景跳过 sparkline）
	cache  *pulseCache
}

func NewPulseService(router *Router, store *sqlite.Store) *PulseService {
	return &PulseService{router: router, store: store, cache: newPulseCache()}
}

// sfBuilt 是 singleflight 闭包的返回载体：valid=false 表示构建者 ctx 在完成时
// 已死亡（产物是降级快照且未入缓存），健康 ctx 的等待者应重发一次。
type sfBuilt struct {
	snap  *PulseSnapshot
	valid bool
}

// Snapshot 返回 view 的市场快照。单资产失败=缺席；源整体失败=全缺席+warning；
// 函数级 error 仅保留给"组合表不存在"这类编程错误。
func (s *PulseService) Snapshot(ctx context.Context, view View, withSpark bool) (*PulseSnapshot, error) {
	refs, ok := viewCompositions[view]
	if !ok {
		return nil, fmt.Errorf("unknown view %q", view)
	}
	key := string(view)
	if withSpark {
		key += "+spark"
	}
	if cached := s.cache.get(key); cached != nil {
		return cached, nil
	}
	build := func() (any, error) {
		// set 在闭包内：恰好一个 goroutine 构建并写缓存，等待者只取结果。
		snap := s.build(ctx, view, refs, withSpark)
		valid := ctx.Err() == nil
		if valid { // 防缓存投毒：ctx 已死的快照不进缓存
			s.cache.set(key, snap)
		}
		return sfBuilt{snap: snap, valid: valid}, nil
	}
	v, _, _ := s.cache.sf.Do(key, build)
	res := v.(sfBuilt)
	if !res.valid && ctx.Err() == nil {
		// 首调 ctx 死亡产出降级快照，本调用方健康：重发一次（首轮 Do 已结束，
		// 此轮会真正重建；并发的健康等待者自然合流到同一次重建）。
		if v2, _, _ := s.cache.sf.Do(key, build); v2 != nil {
			res = v2.(sfBuilt)
		}
	}
	return res.snap, nil
}

func (s *PulseService) build(ctx context.Context, view View, refs []AssetRef, withSpark bool) *PulseSnapshot {
	snap := &PulseSnapshot{SnapshotAt: time.Now().UTC(), View: view}
	groups := s.router.GroupBySource(refs)
	for _, ref := range groups.Unrouted {
		snap.Missing = append(snap.Missing, ref.ID)
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("%s: no source registered for class %s", ref.ID, ref.Class))
	}

	got := map[string]Quote{}
	for src, srcRefs := range groups.Routed {
		quotes, err := src.BatchQuotes(ctx, srcRefs)
		if err != nil {
			for _, r := range srcRefs {
				snap.Missing = append(snap.Missing, r.ID)
			}
			snap.Warnings = append(snap.Warnings, fmt.Sprintf("%s: %v", src.Name(), err))
			continue
		}
		for id, q := range quotes {
			got[id] = q
		}
		for _, r := range srcRefs {
			if _, present := quotes[r.ID]; !present {
				snap.Missing = append(snap.Missing, r.ID)
			}
		}
	}

	// sparkline 补数：先批量（一个 source 一个子进程），装配环节只读库。
	withBars := withSpark && s.store != nil
	if withBars {
		_, lookback := sparkWindow(view)
		s.refillStaleSparkBars(ctx, refs, lookback)
	}

	// 组合表顺序输出 + 视图级 basis 调整。
	for _, ref := range refs {
		q, present := got[ref.ID]
		if !present {
			continue
		}
		q.Basis = adjustBasis(view, ref.Class, q.Basis)
		asset := PulseAsset{Quote: q}
		if withBars {
			asset.Spark, asset.SparkWindow = s.sparkFromStore(ctx, view, ref)
		}
		snap.Assets = append(snap.Assets, asset)
	}
	sort.Strings(snap.Missing)
	return snap
}

// adjustBasis：现货指数/收益率在休市视图下是定格值 —— delayed 升格为 frozen（诚实标注）。
func adjustBasis(view View, class AssetClass, b Basis) Basis {
	if b != BasisDelayed {
		return b
	}
	frozenView := view == ViewPostclose || view == ViewPremarket
	frozenClass := class == ClassIndex || class == ClassYield
	if frozenView && frozenClass {
		return BasisFrozen
	}
	return b
}

// sparkWindow：view → 窗口名与回看时长（premarket 看隔夜，其余看日内）。
func sparkWindow(view View) (string, time.Duration) {
	if view == ViewPremarket {
		return "overnight", 18 * time.Hour
	}
	return "session", 8 * time.Hour
}

// refillStaleSparkBars 预扫描 LastPulseBarTS 过期(>sparkStaleAfter)的资产，
// 按路由 source 分组批量补数：实现 batchBarsSource 的 source 一组一次调用
// （一个子进程），不支持的退化为逐资产 Bars。bar 失败永不阻塞报价（旧数据照用）。
func (s *PulseService) refillStaleSparkBars(ctx context.Context, refs []AssetRef, lookback time.Duration) {
	staleBySource := map[Source][]AssetRef{}
	for _, ref := range refs {
		src, ok := s.router.routes[ref.Class]
		if !ok {
			continue
		}
		last, _ := s.store.LastPulseBarTS(ctx, ref.ID)
		if time.Since(last) > sparkStaleAfter {
			staleBySource[src] = append(staleBySource[src], ref)
		}
	}
	for src, stale := range staleBySource {
		if bb, ok := src.(batchBarsSource); ok {
			barsByID, err := bb.BatchBars(ctx, stale, "5m", lookback)
			if err != nil {
				continue
			}
			for id, bars := range barsByID {
				if len(bars) > 0 {
					_ = s.store.UpsertPulseBars(ctx, id, bars)
				}
			}
			continue
		}
		for _, ref := range stale {
			if bars, err := src.Bars(ctx, ref, "5m", lookback); err == nil && len(bars) > 0 {
				_ = s.store.UpsertPulseBars(ctx, ref.ID, bars)
			}
		}
	}
}

// sparkFromStore 只读 SQLite 取 sparkline（补数已由 refillStaleSparkBars 批量完成）。
func (s *PulseService) sparkFromStore(ctx context.Context, view View, ref AssetRef) ([]float64, string) {
	window, lookback := sparkWindow(view)
	since := time.Now().UTC().Add(-lookback)
	bars, err := s.store.GetPulseBars(ctx, ref.ID, since)
	if err != nil || len(bars) == 0 {
		return nil, ""
	}
	spark := make([]float64, 0, len(bars))
	for _, b := range bars {
		spark = append(spark, b.Close)
	}
	return spark, window
}
