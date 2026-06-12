//go:build integration

package marketdata

import (
	"context"
	"testing"
	"time"
)

// 真 yahoo batch（网络依赖）：拉 3 个跨 class 符号，断言非空 + basis。
// 网络/yfinance 不可用 → Skip 不 Fail（spec §8）。
func TestIntegration_RealYahooBatchQuotes(t *testing.T) {
	src := NewYFinanceSource("python3")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	refs := []AssetRef{
		{ID: "ES", Class: ClassFuture},
		{ID: "VIX", Class: ClassIndex},
		{ID: "USDJPY", Class: ClassFX},
	}
	quotes, err := src.BatchQuotes(ctx, refs)
	if err != nil {
		t.Skipf("yahoo/yfinance unavailable: %v", err)
	}
	if len(quotes) == 0 {
		t.Skip("yahoo returned no data (rate limited or offline)")
	}
	for id, q := range quotes {
		if q.Price <= 0 || q.Basis == "" {
			t.Errorf("%s: bad quote %+v", id, q)
		}
	}
}
