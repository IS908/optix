package eventintel

import (
	"math"
	"time"
)

var sensitivityAssets = []eventAsset{
	{ID: "SPX", Label: "SPX"},
	{ID: "NDX", Label: "NDX"},
	{ID: "VIX", Label: "VIX"},
	{ID: "DXY", Label: "DXY"},
	{ID: "GOLD", Label: "Gold"},
	{ID: "US10Y", Label: "US 10Y"},
}

func BuildSensitivityMatrix(windows map[string][]EventWindowReturn, asOf time.Time) SensitivityDTO {
	out := SensitivityDTO{
		AsOf:   asOf.UTC(),
		Source: "computed from event-window daily returns",
		Rows:   []SensitivityRow{},
	}
	risk := averageEventSeries(seriesFor(windows["SPX"]), seriesFor(windows["NDX"]))
	rates := seriesFor(windows["US10Y"])
	dollar := seriesFor(windows["DXY"])
	for _, asset := range sensitivityAssets {
		series := seriesFor(windows[asset.ID])
		row := SensitivityRow{
			ID:       asset.ID,
			Label:    asset.Label,
			RiskOn:   signedAlignment(series, risk),
			RatesUp:  signedAlignment(series, rates),
			DollarUp: signedAlignment(series, dollar),
			SampleN:  len(series),
		}
		if row.SampleN == 0 {
			row.Note = "insufficient event windows"
			out.Warnings = append(out.Warnings, asset.ID+": no event-window returns")
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}

func seriesFor(windows []EventWindowReturn) map[string]float64 {
	out := make(map[string]float64, len(windows))
	for _, window := range windows {
		out[dayKey(window.EventDate)] = window.EventMovePct
	}
	return out
}

func averageEventSeries(series ...map[string]float64) map[string]float64 {
	sums := map[string]float64{}
	counts := map[string]int{}
	for _, s := range series {
		for date, value := range s {
			sums[date] += value
			counts[date]++
		}
	}
	if len(sums) == 0 {
		return nil
	}
	out := make(map[string]float64, len(sums))
	for date, sum := range sums {
		out[date] = sum / float64(counts[date])
	}
	return out
}

func signedAlignment(asset, driver map[string]float64) float64 {
	if len(asset) == 0 || len(driver) == 0 {
		return 0
	}
	var sum float64
	var used int
	for date, assetMove := range asset {
		driverMove, ok := driver[date]
		if !ok || assetMove == 0 || driverMove == 0 {
			continue
		}
		sum += sign(assetMove) * sign(driverMove)
		used++
	}
	if used == 0 {
		return 0
	}
	return clamp(sum/float64(used), -1, 1)
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	if v > 0 {
		return 1
	}
	return 0
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}
