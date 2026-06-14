package eventintel

import (
	"testing"
	"time"
)

func TestBuildSensitivityMatrixScoresSignedDriverAlignment(t *testing.T) {
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	windows := map[string][]EventWindowReturn{
		"SPX":   {{EventMovePct: 1}, {EventMovePct: -1}},
		"NDX":   {{EventMovePct: 1.4}, {EventMovePct: -1.2}},
		"VIX":   {{EventMovePct: -3}, {EventMovePct: 3}},
		"US10Y": {{EventMovePct: 0.20}, {EventMovePct: 0.30}},
		"DXY":   {{EventMovePct: 0.40}, {EventMovePct: 0.10}},
		"GOLD":  {{EventMovePct: -0.20}, {EventMovePct: 0.20}},
	}

	dto := BuildSensitivityMatrix(windows, now)

	if len(dto.Rows) != len(sensitivityAssets) {
		t.Fatalf("rows = %d, want %d", len(dto.Rows), len(sensitivityAssets))
	}
	spx := findSensitivityRow(t, dto.Rows, "SPX")
	if spx.RiskOn < 0.99 {
		t.Fatalf("SPX risk-on = %.2f, want near +1", spx.RiskOn)
	}
	vix := findSensitivityRow(t, dto.Rows, "VIX")
	if vix.RiskOn > -0.99 {
		t.Fatalf("VIX risk-on = %.2f, want near -1", vix.RiskOn)
	}
	us10y := findSensitivityRow(t, dto.Rows, "US10Y")
	if us10y.RatesUp < 0.99 {
		t.Fatalf("US10Y rates-up = %.2f, want near +1", us10y.RatesUp)
	}
	dxy := findSensitivityRow(t, dto.Rows, "DXY")
	if dxy.DollarUp < 0.99 {
		t.Fatalf("DXY dollar-up = %.2f, want near +1", dxy.DollarUp)
	}
}

func TestBuildSensitivityMatrixAlignsDriversByEventDate(t *testing.T) {
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	d1 := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	windows := map[string][]EventWindowReturn{
		"SPX": {
			{EventDate: d1, EventMovePct: 1},
			{EventDate: d3, EventMovePct: 1},
		},
		"US10Y": {
			{EventDate: d2, EventMovePct: 1},
			{EventDate: d3, EventMovePct: -1},
		},
	}

	dto := BuildSensitivityMatrix(windows, now)

	spx := findSensitivityRow(t, dto.Rows, "SPX")
	if spx.RatesUp > -0.99 {
		t.Fatalf("SPX rates-up = %.2f, want date-aligned -1", spx.RatesUp)
	}
}

func findSensitivityRow(t *testing.T, rows []SensitivityRow, id string) SensitivityRow {
	t.Helper()
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("missing sensitivity row %s", id)
	return SensitivityRow{}
}
