package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/IS908/optix/internal/broker/yfinance"
	"github.com/IS908/optix/pkg/model"
)

// yahooAsset 描述一个业务 ID 在 Yahoo 侧的取数与变换规则。
type yahooAsset struct {
	symbol  string // Yahoo 符号
	label   string // 渲染标签
	basis   Basis  // 静态基础口径（视图级 frozen 调整在 PulseService）
	pctOnly bool   // 只代理涨跌幅（SOX_PROXY）
}

// yahooAssets：业务 ID → Yahoo 规则。实现期逐符号在 yahoo 验证过再入表；
// 欧洲板块指数 ^SX8P 在 Yahoo 不稳定，M1 用 ^STOXX50E（SX5E）代替。
var yahooAssets = map[string]yahooAsset{
	// 指数现货
	"SPX": {symbol: "^GSPC", label: "SPX", basis: BasisDelayed},
	"NDX": {symbol: "^NDX", label: "NDX", basis: BasisDelayed},
	"DJI": {symbol: "^DJI", label: "DJI", basis: BasisDelayed},
	"SOX": {symbol: "^SOX", label: "SOX", basis: BasisDelayed},
	"RTY": {symbol: "^RUT", label: "RTY", basis: BasisDelayed},
	"VIX": {symbol: "^VIX", label: "VIX", basis: BasisDelayed},
	// 期货（Yahoo 免费携带 CME 延迟）
	"ES":    {symbol: "ES=F", label: "ES (S&P fut)", basis: BasisDelayed},
	"NQ":    {symbol: "NQ=F", label: "NQ (Nasdaq fut)", basis: BasisDelayed},
	"YM":    {symbol: "YM=F", label: "YM (Dow fut)", basis: BasisDelayed},
	"RTY_F": {symbol: "RTY=F", label: "RTY (Russell fut)", basis: BasisDelayed},
	"WTI":   {symbol: "CL=F", label: "WTI", basis: BasisDelayed},
	"BRENT": {symbol: "BZ=F", label: "Brent", basis: BasisDelayed},
	"GOLD":  {symbol: "GC=F", label: "黄金", basis: BasisDelayed},
	"US2Y":  {symbol: "2YY=F", label: "US 2Y", basis: BasisDelayed},
	// 收益率指数（Yahoo fast_info + chart API 实测：^TNX/^FVX/^IRX 直接返回百分数形式的收益率，
	// 如 ^TNX=4.467、^FVX=4.19、^IRX=3.62，CBOE ×10 惯例在此不适用；无需缩放，直接使用原值 → approx）
	"US10Y": {symbol: "^TNX", label: "US 10Y", basis: BasisApprox},
	"US5Y":  {symbol: "^FVX", label: "US 5Y", basis: BasisApprox},
	"US13W": {symbol: "^IRX", label: "US 13W", basis: BasisApprox},
	// vol 族
	"VIX9D": {symbol: "^VIX9D", label: "VIX9D", basis: BasisDelayed},
	"VIX3M": {symbol: "^VIX3M", label: "VIX3M", basis: BasisDelayed},
	"OVX":   {symbol: "^OVX", label: "OVX", basis: BasisDelayed},
	// FX
	"DXY":    {symbol: "DX-Y.NYB", label: "DXY", basis: BasisDelayed},
	"USDJPY": {symbol: "JPY=X", label: "美元/日元", basis: BasisDelayed},
	"USDCNH": {symbol: "CNH=X", label: "离岸人民币", basis: BasisDelayed},
	// ETF shock sensors
	"SPY":  {symbol: "SPY", label: "SPY", basis: BasisDelayed},
	"QQQ":  {symbol: "QQQ", label: "QQQ", basis: BasisDelayed},
	"IWM":  {symbol: "IWM", label: "IWM", basis: BasisDelayed},
	"TLT":  {symbol: "TLT", label: "TLT", basis: BasisDelayed},
	"HYG":  {symbol: "HYG", label: "HYG", basis: BasisDelayed},
	"LQD":  {symbol: "LQD", label: "LQD", basis: BasisDelayed},
	"GLD":  {symbol: "GLD", label: "GLD", basis: BasisDelayed},
	"USO":  {symbol: "USO", label: "USO", basis: BasisDelayed},
	"UUP":  {symbol: "UUP", label: "UUP", basis: BasisDelayed},
	"VIXY": {symbol: "VIXY", label: "VIXY", basis: BasisDelayed},
	// 国际/个股代理
	"N225":      {symbol: "^N225", label: "N225", basis: BasisDelayed},
	"SX5E":      {symbol: "^STOXX50E", label: "欧洲 SX5E", basis: BasisDelayed},
	"TSMC_TW":   {symbol: "2330.TW", label: "台积电(台)", basis: BasisDelayed},
	"TSM":       {symbol: "TSM", label: "TSM ADR", basis: BasisDelayed},
	"SOX_PROXY": {symbol: "SOXX", label: "SOX (via SOXX pre-mkt)", basis: BasisApprox, pctOnly: true},
}

// YFinanceSource 通过既有 fetcher.py（batch 子命令）取数。
type YFinanceSource struct{ PythonBin string }

const yfinanceSourceName = "yfinance"

func NewYFinanceSource(pythonBin string) *YFinanceSource {
	return &YFinanceSource{PythonBin: pythonBin}
}

func (s *YFinanceSource) Name() string { return yfinanceSourceName }

func (s *YFinanceSource) BatchQuotes(ctx context.Context, refs []AssetRef) (map[string]Quote, error) {
	symbols := make([]string, 0, len(refs))
	for _, r := range refs {
		if ya, ok := yahooAssets[r.ID]; ok {
			symbols = append(symbols, ya.symbol)
		}
	}
	if len(symbols) == 0 {
		return map[string]Quote{}, nil
	}
	raw, err := yfinance.RunFetcher(ctx, s.PythonBin, "batch_quotes", strings.Join(symbols, ","))
	if err != nil {
		return nil, fmt.Errorf("yfinance batch_quotes: %w", err)
	}
	return parseBatchQuotes(raw, refs)
}

type rawQuote struct {
	Price     float64 `json:"price"`
	Change    float64 `json:"change"`
	ChangePct float64 `json:"change_pct"`
}

// parseBatchQuotes 把 Yahoo 符号键的 payload 映射回业务 ID 并应用 basis。
// payload 中缺席的符号 → 结果缺席（上游失败即缺席契约）。
func parseBatchQuotes(raw []byte, refs []AssetRef) (map[string]Quote, error) {
	var bySymbol map[string]rawQuote
	if err := json.Unmarshal(raw, &bySymbol); err != nil {
		return nil, fmt.Errorf("parse batch_quotes payload: %w", err)
	}
	now := time.Now().UTC()
	out := make(map[string]Quote, len(refs))
	for _, ref := range refs {
		ya, ok := yahooAssets[ref.ID]
		if !ok {
			continue
		}
		rq, present := bySymbol[ya.symbol]
		if !present {
			continue
		}
		q := Quote{
			Ref:       ref,
			Label:     ya.label,
			Price:     rq.Price,
			PctOnly:   ya.pctOnly,
			Change:    rq.Change,
			ChangePct: rq.ChangePct,
			AsOf:      now,
			Basis:     ya.basis,
			Source:    yfinanceSourceName,
		}
		if ya.pctOnly {
			// 代理资产只代理涨跌幅：把代理标的自身的价格清零，防止粗心的
			// 消费方把 SOXX 的 ETF 价格当成 SOX 指数点位渲染出去。
			q.Price = 0
			q.Change = 0
		}
		out[ref.ID] = q
	}
	return out, nil
}

// NewYFinanceRouter 返回所有资产类都路由到 yfinance 的 Router（M1 免费数据形态；
// 订阅源接入后按类改路由）。CLI pulse 与 server intel API 共用，类清单单点维护。
func NewYFinanceRouter(pythonBin string) *Router {
	r := NewRouter()
	yf := NewYFinanceSource(pythonBin)
	for _, c := range []AssetClass{
		ClassIndex, ClassFuture, ClassStock, ClassFX, ClassYield, ClassVol,
	} {
		r.Register(c, yf)
	}
	return r
}

func (s *YFinanceSource) Bars(ctx context.Context, ref AssetRef, interval string, lookback time.Duration) ([]model.OHLCV, error) {
	bars, err := s.BatchBars(ctx, []AssetRef{ref}, interval, lookback)
	if err != nil {
		return nil, err
	}
	return bars[ref.ID], nil
}

// BatchBars 一次取多资产 sparkline bar（PulseService 优先用这个）。
func (s *YFinanceSource) BatchBars(ctx context.Context, refs []AssetRef, interval string, lookback time.Duration) (map[string][]model.OHLCV, error) {
	symToID := map[string]string{}
	symbols := make([]string, 0, len(refs))
	for _, r := range refs {
		if ya, ok := yahooAssets[r.ID]; ok {
			symToID[ya.symbol] = r.ID
			symbols = append(symbols, ya.symbol)
		}
	}
	if len(symbols) == 0 {
		return map[string][]model.OHLCV{}, nil
	}
	period := batchBarsPeriod(lookback)
	raw, err := yfinance.RunFetcher(ctx, s.PythonBin, "batch_bars", strings.Join(symbols, ","), interval, period)
	if err != nil {
		return nil, fmt.Errorf("yfinance batch_bars: %w", err)
	}
	return parseBatchBars(raw, symToID)
}

func batchBarsPeriod(lookback time.Duration) string {
	days := int(lookback.Hours() / 24)
	switch {
	case days <= 1:
		return "1d"
	case days <= 5:
		return "5d"
	case days <= 30:
		return "1mo"
	case days <= 90:
		return "3mo"
	case days <= 180:
		return "6mo"
	case days <= 365:
		return "1y"
	default:
		return "2y"
	}
}

type rawBar struct {
	TS     string  `json:"ts"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

func parseBatchBars(raw []byte, symToID map[string]string) (map[string][]model.OHLCV, error) {
	var bySymbol map[string][]rawBar
	if err := json.Unmarshal(raw, &bySymbol); err != nil {
		return nil, fmt.Errorf("parse batch_bars payload: %w", err)
	}
	out := map[string][]model.OHLCV{}
	for sym, rows := range bySymbol {
		id, ok := symToID[sym]
		if !ok {
			continue
		}
		bars := make([]model.OHLCV, 0, len(rows))
		for _, r := range rows {
			ts, err := parseYahooTimestamp(r.TS)
			if err != nil {
				continue // 单 bar 时间戳坏 → 跳过该 bar，不毁整组
			}
			bars = append(bars, model.OHLCV{
				Timestamp: ts.UTC(), Open: r.Open, High: r.High,
				Low: r.Low, Close: r.Close, Volume: r.Volume,
			})
		}
		if len(bars) > 0 {
			out[id] = bars
		}
	}
	return out, nil
}

func parseYahooTimestamp(raw string) (time.Time, error) {
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts.UTC(), nil
	}
	ts, err := time.Parse("2006-01-02T15:04:05", raw)
	if err != nil {
		return time.Time{}, err
	}
	return ts.UTC(), nil
}
