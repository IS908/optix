package cli

import (
	"testing"
	"time"
)

// TestClampSinceTo7Days locks in the date-aligned boundary semantics around
// IBKR's ReqExecutions 7-day window. The boundary is UTC midnight 7 calendar
// days before `now`, so a --since matching exactly that boundary must pass
// through unchanged — a regression here would re-introduce the spurious-clamp
// bug the live dry-run caught when sevenDaysAgo still carried wall-clock time.
func TestClampSinceTo7Days(t *testing.T) {
	// now: 2026-05-15 14:30 UTC. Boundary: 2026-05-08 00:00 UTC.
	now := time.Date(2026, 5, 15, 14, 30, 0, 0, time.UTC)
	boundary := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		input       time.Time
		wantTime    time.Time
		wantClamped bool
	}{
		{
			name:        "within window: yesterday",
			input:       time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
			wantTime:    time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
			wantClamped: false,
		},
		{
			name:        "window boundary: exactly 7 calendar days ago",
			input:       boundary,
			wantTime:    boundary,
			wantClamped: false,
		},
		{
			name:        "just outside window: 8 days ago",
			input:       time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC),
			wantTime:    boundary,
			wantClamped: true,
		},
		{
			name:        "way outside window: 2020-01-01",
			input:       time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			wantTime:    boundary,
			wantClamped: true,
		},
		{
			name:        "future date stays as-is",
			input:       time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
			wantTime:    time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
			wantClamped: false,
		},
		{
			name:        "exactly now (date portion) stays as-is",
			input:       time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
			wantTime:    time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
			wantClamped: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotClamped := clampSinceTo7Days(tc.input, now)
			if !got.Equal(tc.wantTime) {
				t.Errorf("clamped time = %v, want %v", got, tc.wantTime)
			}
			if gotClamped != tc.wantClamped {
				t.Errorf("wasClamped = %v, want %v", gotClamped, tc.wantClamped)
			}
		})
	}
}
