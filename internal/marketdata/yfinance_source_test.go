package marketdata

import (
	"math"
	"testing"
)

// TODO(M1-Task6): enable when viewCompositions lands (same PR).
// viewCompositions is defined in pulse.go (Commit 4); this test pins mapping
// completeness — every business ID used by a view composition table must have
// a Yahoo mapping. Uncomment verbatim once pulse.go exists.
//
// // 每个组合表用到的业务 ID 必须有 Yahoo 映射 —— 钉死映射完整性。
// func TestYahooMappingCoversAllCompositions(t *testing.T) {
// 	for view, refs := range viewCompositions {
// 		for _, ref := range refs {
// 			if _, ok := yahooAssets[ref.ID]; !ok {
// 				t.Errorf("view %s: asset %q has no yahoo mapping", view, ref.ID)
// 			}
// 		}
// 	}
// }

func TestParseBatchQuotes_MixedSuccessAndMissing(t *testing.T) {
	// US10Y(^TNX) 在 payload 中缺席（取数失败），SPX 正常，US5Y 走 ÷10 scale。
	raw := []byte(`{
		"^GSPC": {"price": 6012.24, "change": -18.5, "change_pct": -0.31},
		"^FVX":  {"price": 41.2,   "change": 0.5,   "change_pct": 1.2}
	}`)
	refs := []AssetRef{
		{ID: "SPX", Class: ClassIndex},
		{ID: "US10Y", Class: ClassYield},
		{ID: "US5Y", Class: ClassYield},
	}
	quotes, err := parseBatchQuotes(raw, refs)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := quotes["US10Y"]; present {
		t.Errorf("US10Y should be ABSENT (fetch failed upstream)")
	}
	if q := quotes["SPX"]; math.Abs(q.Price-6012.24) > 1e-9 || q.Basis != BasisDelayed {
		t.Errorf("SPX = %+v", q)
	}
	// ^FVX 41.2 ÷ 10 = 4.12%，yield 标 approx
	if q := quotes["US5Y"]; math.Abs(q.Price-4.12) > 1e-9 || q.Basis != BasisApprox {
		t.Errorf("US5Y = %+v (want scaled 4.12, approx)", q)
	}
}

func TestParseBatchBars(t *testing.T) {
	raw := []byte(`{"ES=F": [
		{"ts": "2026-06-01T08:00:00-04:00", "open": 6010, "high": 6015, "low": 6008, "close": 6012.5, "volume": 1200}
	]}`)
	bars, err := parseBatchBars(raw, map[string]string{"ES=F": "ES"})
	if err != nil {
		t.Fatal(err)
	}
	if len(bars["ES"]) != 1 || bars["ES"][0].Close != 6012.5 {
		t.Fatalf("bars = %+v", bars)
	}
	if bars["ES"][0].Timestamp.IsZero() {
		t.Fatalf("timestamp not parsed")
	}
}
