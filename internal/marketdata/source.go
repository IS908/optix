// Package marketdata provides multi-asset market snapshots for the Market
// Intel views: business-ID asset refs routed by class to pluggable sources,
// batch-first fetching, and basis (data-quality) labeling. It is a peer of
// the broker package: broker = account/options trading data (IBKR-shaped),
// marketdata = market state sensors. The only cross-dependency is the
// one-way reuse of broker/yfinance's RunFetcher subprocess utility.
package marketdata

import (
	"context"
	"fmt"
	"time"

	"github.com/IS908/optix/pkg/model"
)

type AssetClass string

const (
	ClassIndex  AssetClass = "index"  // SPX/NDX/DJI/SOX/RTY + VIX 现货
	ClassFuture AssetClass = "future" // =F 族（ES/NQ/CL/GC/2YY…）— 将来切 IBKR CME 的类
	ClassStock  AssetClass = "stock"  // 个股/ADR/ETF 代理
	ClassFX     AssetClass = "fx"
	ClassYield  AssetClass = "yield" // 收益率指数（^TNX 等，Yahoo 直接报百分数，无缩放）
	ClassVol    AssetClass = "vol"   // ^VIX9D/^VIX3M/^OVX
)

// AssetRef 用业务 ID（"SPX"/"ES"/"US10Y"），数据源符号（"^GSPC"）是 source 私事。
type AssetRef struct {
	ID    string
	Class AssetClass
}

type Basis string

const (
	BasisRealtime Basis = "realtime"
	BasisDelayed  Basis = "delayed"
	BasisApprox   Basis = "approx" // 代理口径（SOXX 代理 SOX、收益率指数代理国债）
	BasisFrozen   Basis = "frozen" // 收盘定格/隔夜冻结（PulseService 按 view 调整）
)

type Quote struct {
	Ref       AssetRef
	Label     string
	Price     float64 // approx 代理可为 0，只给 ChangePct（诚实优于硬凑）
	PctOnly   bool    // true: Price 无意义，渲染为 —
	Change    float64
	ChangePct float64
	AsOf      time.Time
	Basis     Basis
	Source    string
}

func BasisNote(q Quote) string {
	source := q.Source
	if source == "" {
		source = "unknown source"
	}
	switch q.Basis {
	case BasisRealtime:
		return fmt.Sprintf("%s realtime quote", source)
	case BasisDelayed:
		return fmt.Sprintf("%s delayed quote", source)
	case BasisFrozen:
		return fmt.Sprintf("%s frozen from the last delayed regular-session quote", source)
	case BasisApprox:
		if q.PctOnly && q.Ref.ID == "SOX_PROXY" {
			return fmt.Sprintf("%s approx: SOXX ETF pct-only proxy for SOX premarket; no SOX index level", source)
		}
		if q.Ref.Class == ClassYield {
			return fmt.Sprintf("%s approx: Yahoo/CBOE yield-index proxy; not FRED/Treasury official close", source)
		}
		return fmt.Sprintf("%s approx proxy quote", source)
	default:
		return source
	}
}

// Source 是数据源实现接口，批量优先。不支持/失败的 ref 在结果 map 缺席（不报错）。
type Source interface {
	Name() string
	BatchQuotes(ctx context.Context, refs []AssetRef) (map[string]Quote, error)
	Bars(ctx context.Context, ref AssetRef, interval string, lookback time.Duration) ([]model.OHLCV, error)
}

// Router 按 AssetClass → Source 路由。现在全部注册到 yfinance；将来
// ClassFuture/ClassIndex 切 IBKR（CME 订阅后）时调用方零改动。
type Router struct{ routes map[AssetClass]Source }

func NewRouter() *Router { return &Router{routes: map[AssetClass]Source{}} }

func (r *Router) Register(class AssetClass, src Source) { r.routes[class] = src }

// RouteGroups 是按 source 分桶的结果；Unrouted 保留给调用方降级处理。
type RouteGroups struct {
	Routed   map[Source][]AssetRef
	Unrouted []AssetRef
}

func (r *Router) GroupBySource(refs []AssetRef) RouteGroups {
	g := RouteGroups{Routed: map[Source][]AssetRef{}}
	for _, ref := range refs {
		src, ok := r.routes[ref.Class]
		if !ok {
			g.Unrouted = append(g.Unrouted, ref)
			continue
		}
		g.Routed[src] = append(g.Routed[src], ref)
	}
	return g
}
