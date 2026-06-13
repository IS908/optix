package eventintel

import (
	"testing"
	"time"

	"github.com/IS908/optix/internal/marketdata"
	"github.com/IS908/optix/pkg/model"
)

func TestBuildRatePathUsesBarBaselineBeforeQuoteFallback(t *testing.T) {
	now := time.Date(2026, 6, 13, 13, 30, 0, 0, time.UTC)
	quotes := map[string]marketdata.Quote{
		"US10Y": {
			Ref:       marketdata.AssetRef{ID: "US10Y", Class: marketdata.ClassYield},
			Label:     "US 10Y",
			Price:     4.25,
			Change:    0.10,
			ChangePct: 2.41,
			AsOf:      now,
			Basis:     marketdata.BasisApprox,
		},
		"SPX": {
			Ref:       marketdata.AssetRef{ID: "SPX", Class: marketdata.ClassIndex},
			Label:     "SPX",
			Price:     5100,
			Change:    50,
			ChangePct: 0.99,
			AsOf:      now,
			Basis:     marketdata.BasisDelayed,
		},
	}
	bars := map[string][]model.OHLCV{
		"US10Y": {
			{Timestamp: now.Add(-2 * time.Hour), Close: 4.00},
			{Timestamp: now.Add(-time.Hour), Close: 4.10},
		},
	}

	dto := BuildRatePath(quotes, bars, now)

	if len(dto.Rows) != len(ratePathAssets) {
		t.Fatalf("rows = %d, want %d", len(dto.Rows), len(ratePathAssets))
	}
	us10y := findRateRow(t, dto.Rows, "US10Y")
	if us10y.PreEvent != 4.00 {
		t.Fatalf("US10Y pre-event = %.2f, want 4.00", us10y.PreEvent)
	}
	if us10y.Change != 0.25 {
		t.Fatalf("US10Y change = %.2f, want 0.25", us10y.Change)
	}
	if us10y.Source != "yfinance" || us10y.Basis != "approx" {
		t.Fatalf("US10Y source/basis = %s/%s, want yfinance/approx", us10y.Source, us10y.Basis)
	}

	spx := findRateRow(t, dto.Rows, "SPX")
	if spx.PreEvent != 5050 {
		t.Fatalf("SPX fallback pre-event = %.2f, want 5050.00", spx.PreEvent)
	}
	if spx.ChangePct < 0.98 || spx.ChangePct > 1.00 {
		t.Fatalf("SPX change pct = %.4f, want about 0.99", spx.ChangePct)
	}
	if len(dto.Warnings) == 0 {
		t.Fatalf("expected warnings for missing assets")
	}
}

func findRateRow(t *testing.T, rows []RatePathRow, id string) RatePathRow {
	t.Helper()
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("missing rate row %s", id)
	return RatePathRow{}
}
