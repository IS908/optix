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

// RawBatchBars 取原始 Yahoo ticker 的 bar（不走 AssetRef registry —— 盘前异动卡用，
// 标的是任意个股代码）。interval/period 同 fetcher.py batch_bars（prepost=True，含盘前）。
func RawBatchBars(ctx context.Context, pythonBin string, symbols []string, interval, period string) (map[string][]model.OHLCV, error) {
	clean := make([]string, 0, len(symbols))
	for _, s := range symbols {
		if t := strings.TrimSpace(s); t != "" {
			clean = append(clean, strings.ToUpper(t))
		}
	}
	if len(clean) == 0 {
		return map[string][]model.OHLCV{}, nil
	}
	raw, err := yfinance.RunFetcher(ctx, pythonBin, "batch_bars", strings.Join(clean, ","), interval, period)
	if err != nil {
		return nil, fmt.Errorf("raw batch_bars: %w", err)
	}
	return parseRawBatchBars(raw)
}

// parseRawBatchBars 解析 batch_bars 输出，键即原始 ticker（identity 映射）。
func parseRawBatchBars(raw []byte) (map[string][]model.OHLCV, error) {
	var bySymbol map[string][]rawBar // rawBar：yfinance_source.go 同包私有
	if err := json.Unmarshal(raw, &bySymbol); err != nil {
		return nil, fmt.Errorf("parse raw batch_bars: %w", err)
	}
	out := map[string][]model.OHLCV{}
	for sym, rows := range bySymbol {
		bars := make([]model.OHLCV, 0, len(rows))
		for _, r := range rows {
			ts, err := time.Parse(time.RFC3339, r.TS)
			if err != nil {
				continue
			}
			bars = append(bars, model.OHLCV{
				Timestamp: ts.UTC(), Open: r.Open, High: r.High,
				Low: r.Low, Close: r.Close, Volume: r.Volume,
			})
		}
		if len(bars) > 0 {
			out[sym] = bars
		}
	}
	return out, nil
}
