package marketdata

import (
	"context"
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func TestLookupAssetRef(t *testing.T) {
	ref, ok := LookupAssetRef("SPX")
	if !ok || ref.ID != "SPX" || ref.Class != ClassIndex {
		t.Errorf("SPX → %+v ok=%v", ref, ok)
	}
	for _, tc := range []struct {
		id    string
		class AssetClass
	}{
		{id: "SPY", class: ClassStock},
		{id: "QQQ", class: ClassStock},
		{id: "VIXY", class: ClassStock},
		{id: "GOLD", class: ClassFuture},
		{id: "US10Y", class: ClassYield},
	} {
		ref, ok := LookupAssetRef(tc.id)
		if !ok || ref.ID != tc.id || ref.Class != tc.class {
			t.Errorf("%s → %+v ok=%v, want class %s", tc.id, ref, ok, tc.class)
		}
	}
	if _, ok := LookupAssetRef("NOPE"); ok {
		t.Error("unknown id must be ok=false")
	}
}

// fakeQuoteSource 返回固定报价。
type fakeQuoteSource struct{ pctOnly bool }

func (f fakeQuoteSource) Name() string { return "fake" }
func (f fakeQuoteSource) BatchQuotes(_ context.Context, refs []AssetRef) (map[string]Quote, error) {
	out := map[string]Quote{}
	for _, r := range refs {
		out[r.ID] = Quote{Ref: r, Label: r.ID, Price: 4200, PctOnly: f.pctOnly, Basis: BasisDelayed}
	}
	return out, nil
}
func (f fakeQuoteSource) Bars(context.Context, AssetRef, string, time.Duration) ([]model.OHLCV, error) {
	return nil, nil
}

func TestQuoteByID(t *testing.T) {
	r := NewRouter()
	r.Register(ClassIndex, fakeQuoteSource{})
	svc := NewPulseService(r, nil)
	q, err := svc.QuoteByID(context.Background(), "SPX")
	if err != nil || q.Price != 4200 {
		t.Fatalf("QuoteByID SPX = %+v err=%v", q, err)
	}
	if _, err := svc.QuoteByID(context.Background(), "NOPE"); err == nil {
		t.Error("unknown asset must error")
	}
}

func TestQuoteByID_PctOnlyRejected(t *testing.T) {
	r := NewRouter()
	r.Register(ClassStock, fakeQuoteSource{pctOnly: true})
	svc := NewPulseService(r, nil)
	// SOX_PROXY 是 pctOnly 代理：无价，不可对账 → 报错。
	if _, err := svc.QuoteByID(context.Background(), "SOX_PROXY"); err == nil {
		t.Error("pct-only asset must error (no price to judge)")
	}
}
