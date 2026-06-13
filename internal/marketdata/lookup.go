package marketdata

import (
	"context"
	"fmt"
)

// assetClassByID 从 viewCompositions 派生 业务ID→AssetClass（class 的权威来源是组合表里的
// AssetRef）。判断只能针对在某视图组合中出现过的资产（即 class 已知的那批）。
var assetClassByID = func() map[string]AssetClass {
	m := map[string]AssetClass{}
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
