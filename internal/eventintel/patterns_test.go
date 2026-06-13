package eventintel

import (
	"testing"
	"time"

	"github.com/IS908/optix/pkg/model"
)

func TestBuildEventPatternsAggregatesTMinusOneToTPlusOne(t *testing.T) {
	events := []EventDate{
		{Date: dateUTC(2026, 5, 6), Kind: "FOMC", Label: "May FOMC"},
		{Date: dateUTC(2026, 6, 10), Kind: "CPI", Label: "Jun CPI"},
	}
	bars := map[string][]model.OHLCV{
		"SPX": {
			dailyBar(2026, 5, 5, 100),
			dailyBar(2026, 5, 6, 102),
			dailyBar(2026, 5, 7, 103),
			dailyBar(2026, 6, 9, 200),
			dailyBar(2026, 6, 10, 204),
			dailyBar(2026, 6, 11, 206),
		},
	}

	dto := BuildEventPatterns(bars, events, time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC))

	if len(dto.Rows) == 0 {
		t.Fatal("expected pattern rows")
	}
	spx := findPatternRow(t, dto.Rows, "SPX")
	if spx.SampleN != 2 {
		t.Fatalf("sample n = %d, want 2", spx.SampleN)
	}
	if spx.AvgEventMovePct < 1.99 || spx.AvgEventMovePct > 2.01 {
		t.Fatalf("avg event move = %.4f, want about 2.00", spx.AvgEventMovePct)
	}
	if spx.AvgNextMovePct < 0.98 || spx.AvgNextMovePct > 0.99 {
		t.Fatalf("avg next move = %.4f, want about 0.985", spx.AvgNextMovePct)
	}
	if spx.DirectionalConsistency != 1 {
		t.Fatalf("consistency = %.2f, want 1.00", spx.DirectionalConsistency)
	}
}

func findPatternRow(t *testing.T, rows []EventPatternRow, id string) EventPatternRow {
	t.Helper()
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("missing pattern row %s", id)
	return EventPatternRow{}
}

func dailyBar(year int, month time.Month, day int, close float64) model.OHLCV {
	return model.OHLCV{Timestamp: dateUTC(year, month, day), Close: close}
}

func dateUTC(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
