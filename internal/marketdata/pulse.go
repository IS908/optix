package marketdata

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/IS908/optix/internal/datastore/sqlite"
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

type PulseService struct {
	router *Router
	store  *sqlite.Store // nil 容忍（测试/无库场景跳过 sparkline）
	cache  *pulseCache
}

func NewPulseService(router *Router, store *sqlite.Store) *PulseService {
	return &PulseService{router: router, store: store, cache: newPulseCache()}
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
	v, err, _ := s.cache.sf.Do(key, func() (any, error) {
		return s.build(ctx, view, refs, withSpark), nil
	})
	if err != nil {
		return nil, err
	}
	snap := v.(*PulseSnapshot)
	s.cache.set(key, snap)
	return snap, nil
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

	// 组合表顺序输出 + 视图级 basis 调整。
	for _, ref := range refs {
		q, present := got[ref.ID]
		if !present {
			continue
		}
		q.Basis = adjustBasis(view, ref.Class, q.Basis)
		asset := PulseAsset{Quote: q}
		if withSpark && s.store != nil {
			asset.Spark, asset.SparkWindow = s.sparkFor(ctx, view, ref)
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

// sparkFor：读库存 sparkline；过期(>10min)且 source 可补则增量补。
// bar 失败永不阻塞报价（旧数据照用）。
func (s *PulseService) sparkFor(ctx context.Context, view View, ref AssetRef) ([]float64, string) {
	window := "session"
	lookback := 8 * time.Hour
	if view == ViewPremarket {
		window = "overnight"
		lookback = 18 * time.Hour
	}
	since := time.Now().UTC().Add(-lookback)

	last, _ := s.store.LastPulseBarTS(ctx, ref.ID)
	if time.Since(last) > 10*time.Minute {
		if src, ok := s.router.routes[ref.Class]; ok {
			if bars, err := src.Bars(ctx, ref, "5m", lookback); err == nil && len(bars) > 0 {
				_ = s.store.UpsertPulseBars(ctx, ref.ID, bars)
			}
		}
	}
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
