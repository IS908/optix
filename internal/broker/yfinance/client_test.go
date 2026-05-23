package yfinance

import (
	"testing"
	"time"
)

// TestParseBarsJSON locks the contract for #31: bars whose timestamp can't
// be parsed by any known layout MUST be dropped (not appended with a zero
// Time). Letting them through pollutes the SQLite dedup key, which uses
// `Timestamp.UTC().Truncate(24h).Format(RFC3339)` — every bad bar collapses
// to "0001-01-01T00:00:00Z" and INSERT OR REPLACE silently overwrites them
// against each other across symbols.
func TestParseBarsJSON(t *testing.T) {
	t.Run("happy path RFC3339 timestamps", func(t *testing.T) {
		data := []byte(`[
			{"timestamp":"2026-05-20T14:30:00Z","open":1,"high":2,"low":0.5,"close":1.5,"volume":100},
			{"timestamp":"2026-05-21T14:30:00Z","open":1.5,"high":2.5,"low":1,"close":2,"volume":200}
		]`)
		bars, err := parseBarsJSON(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(bars) != 2 {
			t.Fatalf("want 2 bars, got %d", len(bars))
		}
		want := time.Date(2026, 5, 20, 14, 30, 0, 0, time.UTC)
		if !bars[0].Timestamp.Equal(want) {
			t.Errorf("bar[0].Timestamp = %v, want %v", bars[0].Timestamp, want)
		}
		if bars[0].Open != 1 || bars[0].Close != 1.5 || bars[0].Volume != 100 {
			t.Errorf("bar[0] OHLCV mismatch: %+v", bars[0])
		}
	})

	t.Run("offset suffix layout is parsed", func(t *testing.T) {
		// yfinance has historically emitted timestamps with a numeric tz offset
		// rather than UTC Z. Keep parity with the original two-layout logic.
		data := []byte(`[{"timestamp":"2026-05-20T10:30:00-04:00","open":1,"high":2,"low":0.5,"close":1.5,"volume":100}]`)
		bars, err := parseBarsJSON(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(bars) != 1 {
			t.Fatalf("want 1 bar, got %d", len(bars))
		}
		// 10:30 -04:00 == 14:30 UTC
		want := time.Date(2026, 5, 20, 14, 30, 0, 0, time.UTC)
		if !bars[0].Timestamp.UTC().Equal(want) {
			t.Errorf("bar[0].Timestamp.UTC() = %v, want %v", bars[0].Timestamp.UTC(), want)
		}
	})

	t.Run("bars with unparseable timestamps are dropped, not stored as zero", func(t *testing.T) {
		// This is the #31 regression case. Pre-fix, the second entry was
		// appended with Timestamp = time.Time{} (year 0001) and downstream
		// SQLite dedup collapsed every such bad bar onto the same row.
		data := []byte(`[
			{"timestamp":"2026-05-20T14:30:00Z","open":1,"high":2,"low":0.5,"close":1.5,"volume":100},
			{"timestamp":"not-a-real-timestamp","open":9,"high":9,"low":9,"close":9,"volume":900},
			{"timestamp":"","open":8,"high":8,"low":8,"close":8,"volume":800}
		]`)
		bars, err := parseBarsJSON(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(bars) != 1 {
			t.Fatalf("want 1 bar (only the good one), got %d; bad bars must be dropped", len(bars))
		}
		if bars[0].Volume != 100 {
			t.Errorf("kept the wrong bar; got volume %d, want 100", bars[0].Volume)
		}
		for _, b := range bars {
			if b.Timestamp.IsZero() {
				t.Errorf("kept a zero-timestamp bar: %+v — would poison SQLite dedup", b)
			}
		}
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		_, err := parseBarsJSON([]byte(`{not json at all`))
		if err == nil {
			t.Fatal("expected error on malformed JSON, got nil")
		}
	})

	t.Run("empty array returns empty slice", func(t *testing.T) {
		bars, err := parseBarsJSON([]byte(`[]`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(bars) != 0 {
			t.Fatalf("want 0 bars, got %d", len(bars))
		}
	})
}
