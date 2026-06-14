package marketdata

import (
	"context"
	"fmt"
)

// sourceAssetClassByID records source-known sensors that are not always shown
// in the top-level pulse compositions but are consumed by deeper intel cards.
var sourceAssetClassByID = map[string]AssetClass{
	"SPY":  ClassStock,
	"QQQ":  ClassStock,
	"IWM":  ClassStock,
	"TLT":  ClassStock,
	"HYG":  ClassStock,
	"LQD":  ClassStock,
	"GLD":  ClassStock,
	"USO":  ClassStock,
	"UUP":  ClassStock,
	"VIXY": ClassStock,

	"BRENT":  ClassFuture,
	"US5Y":   ClassYield,
	"US13W":  ClassYield,
	"VIX9D":  ClassVol,
	"VIX3M":  ClassVol,
	"OVX":    ClassVol,
	"USDJPY": ClassFX,
	"USDCNH": ClassFX,

	"N225":      ClassIndex,
	"SX5E":      ClassIndex,
	"TSMC_TW":   ClassStock,
	"TSM":       ClassStock,
	"SOX_PROXY": ClassStock,
}

// assetClassByID 从 viewCompositions 和 sourceAssetClassByID 派生业务 ID→AssetClass。
// 视图组合定义 UI surface，sourceAssetClassByID 补足深层卡片直接消费的传感器资产。
var assetClassByID = func() map[string]AssetClass {
	m := map[string]AssetClass{}
	for id, class := range sourceAssetClassByID {
		m[id] = class
	}
	for _, refs := range viewCompositions {
		for _, r := range refs {
			m[r.ID] = r.Class
		}
	}
	return m
}()

// LookupAssetRef 解析业务 ID → AssetRef（ID+Class）；未知 ID → ok=false。
func LookupAssetRef(id string) (AssetRef, bool) {
	c, ok := assetClassByID[id]
	if !ok {
		return AssetRef{}, false
	}
	return AssetRef{ID: id, Class: c}, true
}

// QuoteByID 抓单资产当前报价（intel judge 登记价用，optix 独占价格）。
// 未知/无路由/无报价/pctOnly(无价不可对账) → 错误。复用 router + Source.BatchQuotes，
// 命中 PulseService 之外的轻量路径（不进 60s 视图缓存；judge 调用频率低）。
func (s *PulseService) QuoteByID(ctx context.Context, id string) (Quote, error) {
	ref, ok := LookupAssetRef(id)
	if !ok {
		return Quote{}, fmt.Errorf("unknown asset %q", id)
	}
	groups := s.router.GroupBySource([]AssetRef{ref})
	if len(groups.Unrouted) > 0 {
		return Quote{}, fmt.Errorf("no source registered for %s (class %s)", id, ref.Class)
	}
	for src, refs := range groups.Routed {
		quotes, err := src.BatchQuotes(ctx, refs)
		if err != nil {
			return Quote{}, fmt.Errorf("quote %s: %w", id, err)
		}
		q, present := quotes[id]
		if !present {
			return Quote{}, fmt.Errorf("no quote for %s", id)
		}
		if q.PctOnly {
			return Quote{}, fmt.Errorf("%s is a pct-only proxy (no price to judge)", id)
		}
		return q, nil
	}
	return Quote{}, fmt.Errorf("no quote for %s", id)
}
