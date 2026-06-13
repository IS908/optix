package eventintel

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

var patternAssets = []eventAsset{
	{ID: "SPX", Label: "SPX", Class: marketdata.ClassIndex},
	{ID: "NDX", Label: "NDX", Class: marketdata.ClassIndex},
	{ID: "VIX", Label: "VIX", Class: marketdata.ClassIndex},
	{ID: "US10Y", Label: "US 10Y", Class: marketdata.ClassYield},
	{ID: "DXY", Label: "DXY", Class: marketdata.ClassFX},
	{ID: "GOLD", Label: "Gold", Class: marketdata.ClassFuture},
}

func BuildEventPatterns(bars map[string][]model.OHLCV, events []EventDate, asOf time.Time) EventPatternsDTO {
	out := EventPatternsDTO{
		AsOf:   asOf.UTC(),
		Source: "yfinance daily bars + built-in event calendar",
		Rows:   []EventPatternRow{},
	}
	for _, asset := range patternAssets {
		windows := ExtractEventWindowReturns(bars[asset.ID], events)
		row := BuildPatternRow(asset.ID, asset.Label, windows)
		out.Rows = append(out.Rows, row)
		if row.SampleN == 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s: no complete event windows", asset.ID))
		}
	}
	return out
}

func BuildPatternRow(id, label string, windows []EventWindowReturn) EventPatternRow {
	row := EventPatternRow{
		ID:     id,
		Label:  label,
		Source: "yfinance",
		Basis:  "historical",
	}
	if len(windows) == 0 {
		row.Note = "insufficient T-1/T/T+1 bars"
		return row
	}
	row.SampleN = len(windows)
	var daySum, nextSum float64
	pos, neg := 0, 0
	for _, window := range windows {
		daySum += window.EventMovePct
		nextSum += window.NextMovePct
		switch {
		case window.EventMovePct > 0:
			pos++
		case window.EventMovePct < 0:
			neg++
		}
	}
	row.AvgEventMovePct = daySum / float64(len(windows))
	row.AvgNextMovePct = nextSum / float64(len(windows))
	denom := pos + neg
	if denom > 0 {
		row.DirectionalConsistency = float64(maxInt(pos, neg)) / float64(denom)
	}
	return row
}

func ExtractEventWindowReturns(bars []model.OHLCV, events []EventDate) []EventWindowReturn {
	if len(bars) < 3 || len(events) == 0 {
		return nil
	}
	sorted := append([]model.OHLCV(nil), bars...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp.Before(sorted[j].Timestamp) })
	byDate := map[string]int{}
	for i, bar := range sorted {
		byDate[dayKey(bar.Timestamp)] = i
	}
	out := []EventWindowReturn{}
	for _, event := range events {
		idx, ok := byDate[dayKey(event.Date)]
		if !ok || idx == 0 || idx+1 >= len(sorted) {
			continue
		}
		prev, cur, next := sorted[idx-1], sorted[idx], sorted[idx+1]
		if prev.Close <= 0 || cur.Close <= 0 {
			continue
		}
		out = append(out, EventWindowReturn{
			EventDate:    event.Date.UTC(),
			Kind:         event.Kind,
			Label:        event.Label,
			EventMovePct: pctChange(prev.Close, cur.Close),
			NextMovePct:  pctChange(cur.Close, next.Close),
		})
	}
	return out
}

func pctChange(from, to float64) float64 {
	if from == 0 {
		return 0
	}
	return (to - from) / math.Abs(from) * 100
}

func dayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
