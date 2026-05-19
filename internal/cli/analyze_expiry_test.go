package cli

import (
	"testing"
	"time"
)

func TestParseExpiryAccepts(t *testing.T) {
	got, err := parseExpiryFlag("2026-05-22", time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "20260522" {
		t.Errorf("got %q, want 20260522", got)
	}
}

func TestParseExpiryEmpty(t *testing.T) {
	got, err := parseExpiryFlag("", time.Now())
	if err != nil || got != "" {
		t.Errorf("got (%q, %v); want (\"\", nil)", got, err)
	}
}

func TestParseExpiryRejectsBadFormat(t *testing.T) {
	_, err := parseExpiryFlag("20260522", time.Now())
	if err == nil {
		t.Errorf("expected error for non-dashed format")
	}
}

func TestParseExpiryRejectsPast(t *testing.T) {
	now := time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC)
	_, err := parseExpiryFlag("2026-05-15", now)
	if err == nil {
		t.Errorf("expected error for past date")
	}
}

func TestParseExpiryAllowsToday(t *testing.T) {
	now := time.Date(2026, 5, 16, 14, 0, 0, 0, time.UTC)
	got, err := parseExpiryFlag("2026-05-16", now)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != "20260516" {
		t.Errorf("got %q, want 20260516", got)
	}
}
