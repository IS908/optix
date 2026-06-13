package marketdata

import (
	"math"
	"strings"
	"testing"
	"time"
)

// 每个组合表用到的业务 ID 必须有 Yahoo 映射 —— 钉死映射完整性。
func TestYahooMappingCoversAllCompositions(t *testing.T) {
	for view, refs := range viewCompositions {
		for _, ref := range refs {
			if _, ok := yahooAssets[ref.ID]; !ok {
				t.Errorf("view %s: asset %q has no yahoo mapping", view, ref.ID)
			}
		}
	}
}

// fetcher.py main() 对 argv[2] 做 `.upper()` 再拆分，而 Go 解析端按原样
// `ya.symbol` 取回包键 —— 整条链路成立的前提是映射里的 Yahoo 符号本身
// 全部大写。钉死该大小写稳定性不变量，防止新增小写符号悄悄破坏匹配。
func TestYahooSymbolsAreUpperCaseStable(t *testing.T) {
	for id, ya := range yahooAssets {
		if strings.ToUpper(ya.symbol) != ya.symbol {
			t.Errorf("asset %q: yahoo symbol %q is not upper-case stable; "+
				"fetcher.py upper-cases argv and responses are keyed by the exact symbol",
				id, ya.symbol)
		}
	}
}

func TestParseBatchQuotes_MixedSuccessAndMissing(t *testing.T) {
	// US10Y(^TNX) 在 payload 中缺席（取数失败），SPX 正常，US5Y 直接透传收益率原值。
	// Yahoo fast_info + chart API 实测：^FVX 返回直接百分数（如 4.19），无需缩放。
	raw := []byte(`{
		"^GSPC": {"price": 6012.24, "change": -18.5,  "change_pct": -0.31},
		"^FVX":  {"price": 4.19,   "change": -0.074, "change_pct": -1.74}
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
	// ^FVX 直接透传：4.19（不缩放），basis=approx（CBOE 指数仍为国债收益率代理）。
	if q := quotes["US5Y"]; math.Abs(q.Price-4.19) > 1e-9 || q.Basis != BasisApprox {
		t.Errorf("US5Y = %+v (want direct yield 4.19, approx)", q)
	}
	// Price/Change/ChangePct 均直接透传，无缩放。
	if q := quotes["US5Y"]; math.Abs(q.Change-(-0.074)) > 1e-9 || math.Abs(q.ChangePct-(-1.74)) > 1e-9 {
		t.Errorf("US5Y Change=%v ChangePct=%v, want -0.074 / -1.74", q.Change, q.ChangePct)
	}
}

// pctOnly 代理资产（SOX_PROXY via SOXX）：只代理涨跌幅，Price/Change 必须清零 —
// 防止消费方把 SOXX 的 ETF 价格当 SOX 指数点位渲染。
func TestParseBatchQuotes_PctOnlyZeroesPrice(t *testing.T) {
	raw := []byte(`{"SOXX": {"price": 231.5, "change": -3.2, "change_pct": -1.38}}`)
	quotes, err := parseBatchQuotes(raw, []AssetRef{{ID: "SOX_PROXY", Class: ClassStock}})
	if err != nil {
		t.Fatal(err)
	}
	q, present := quotes["SOX_PROXY"]
	if !present {
		t.Fatal("SOX_PROXY should be present")
	}
	if !q.PctOnly || q.Price != 0 || q.Change != 0 {
		t.Errorf("pctOnly proxy must zero Price/Change: %+v", q)
	}
	if math.Abs(q.ChangePct-(-1.38)) > 1e-9 {
		t.Errorf("ChangePct = %v, want -1.38", q.ChangePct)
	}
}

func TestParseBatchBars(t *testing.T) {
	raw := []byte(`{"ES=F": [
		{"ts": "2026-06-01T08:00:00-04:00", "open": 6010, "high": 6015, "low": 6008, "close": 6012.5, "volume": 1200},
		{"ts": "2026-06-02T00:00:00", "open": 6020, "high": 6025, "low": 6018, "close": 6022.5, "volume": 1300}
	]}`)
	bars, err := parseBatchBars(raw, map[string]string{"ES=F": "ES"})
	if err != nil {
		t.Fatal(err)
	}
	if len(bars["ES"]) != 2 || bars["ES"][0].Close != 6012.5 || bars["ES"][1].Close != 6022.5 {
		t.Fatalf("bars = %+v", bars)
	}
	if bars["ES"][0].Timestamp.IsZero() || bars["ES"][1].Timestamp.IsZero() {
		t.Fatalf("timestamp not parsed")
	}
	if got := bars["ES"][1].Timestamp.Format(time.RFC3339); got != "2026-06-02T00:00:00Z" {
		t.Fatalf("timezone-less daily timestamp = %s, want UTC", got)
	}
}

func TestBatchBarsPeriodForLookback(t *testing.T) {
	tests := []struct {
		name     string
		lookback time.Duration
		want     string
	}{
		{name: "intraday sparkline", lookback: 8 * time.Hour, want: "1d"},
		{name: "multi day sparkline", lookback: 72 * time.Hour, want: "5d"},
		{name: "one month", lookback: 20 * 24 * time.Hour, want: "1mo"},
		{name: "three months", lookback: 80 * 24 * time.Hour, want: "3mo"},
		{name: "six months", lookback: 150 * 24 * time.Hour, want: "6mo"},
		{name: "one year", lookback: 300 * 24 * time.Hour, want: "1y"},
		{name: "event window history", lookback: 540 * 24 * time.Hour, want: "2y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := batchBarsPeriod(tt.lookback); got != tt.want {
				t.Fatalf("period = %q, want %q", got, tt.want)
			}
		})
	}
}
