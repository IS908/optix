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
	risk := averageSeries(seriesFor(windows["SPX"]), seriesFor(windows["NDX"]))
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

func seriesFor(windows []EventWindowReturn) []float64 {
	out := make([]float64, 0, len(windows))
	for _, window := range windows {
		out = append(out, window.EventMovePct)
	}
	return out
}

func averageSeries(series ...[]float64) []float64 {
	n := 0
	for _, s := range series {
		if len(s) == 0 {
			continue
		}
		if n == 0 || len(s) < n {
			n = len(s)
		}
	}
	if n == 0 {
		return nil
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		var sum float64
		var count int
		for _, s := range series {
			if len(s) > i {
				sum += s[i]
				count++
			}
		}
		if count > 0 {
			out[i] = sum / float64(count)
		}
	}
	return out
}

func signedAlignment(asset, driver []float64) float64 {
	n := minInt(len(asset), len(driver))
	if n == 0 {
		return 0
	}
	var sum float64
	var used int
	for i := 0; i < n; i++ {
		if asset[i] == 0 || driver[i] == 0 {
			continue
		}
		sum += sign(asset[i]) * sign(driver[i])
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
