package marketdata

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

type fakeSource struct{ name string }

func (f *fakeSource) Name() string { return f.name }
func (f *fakeSource) BatchQuotes(_ context.Context, refs []AssetRef) (map[string]Quote, error) {
	out := map[string]Quote{}
	for _, r := range refs {
		out[r.ID] = Quote{Ref: r, Price: 100, Basis: BasisDelayed, AsOf: time.Now()}
	}
	return out, nil
}
func (f *fakeSource) Bars(context.Context, AssetRef, string, time.Duration) ([]model.OHLCV, error) {
	return nil, nil
}

func TestRouterRoutesByClass(t *testing.T) {
	a, b := &fakeSource{"a"}, &fakeSource{"b"}
	r := NewRouter()
	r.Register(ClassIndex, a)
	r.Register(ClassFuture, b)

	groups := r.GroupBySource([]AssetRef{
		{ID: "SPX", Class: ClassIndex},
		{ID: "ES", Class: ClassFuture},
		{ID: "VIX", Class: ClassIndex},
		{ID: "USDJPY", Class: ClassFX}, // 未注册 → 落 unrouted
	})
	if len(groups.Routed[a]) != 2 || len(groups.Routed[b]) != 1 {
		t.Fatalf("routed = %+v", groups.Routed)
	}
	if len(groups.Unrouted) != 1 || groups.Unrouted[0].ID != "USDJPY" {
		t.Fatalf("unrouted = %+v", groups.Unrouted)
	}
}
