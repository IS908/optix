package cli

import (
	"strings"
	"testing"
)

func TestFormatExpiryErrorSortsByDistance(t *testing.T) {
	out := FormatExpiryError("GOOGL", "20260523", []string{
		"20260606", "20260520", "20260522", "20260530", "20260620", "20260718",
	}, "optix analyze GOOGL --with-oi --expiry %s")

	// Expect closest-first order: 5/22 (1d), 5/20 (3d), 5/30 (7d), 6/06 (14d), 6/20 (28d), 7/18 (56d)
	wantOrder := []string{"2026-05-22", "2026-05-20", "2026-05-30", "2026-06-06", "2026-06-20", "2026-07-18"}
	prev := -1
	for _, exp := range wantOrder {
		idx := strings.Index(out, exp)
		if idx < 0 {
			t.Fatalf("expiry %s missing from output:\n%s", exp, out)
		}
		if idx <= prev {
			t.Errorf("expected %s after position %d, got %d:\n%s", exp, prev, idx, out)
		}
		prev = idx
	}
	if !strings.Contains(out, "Did you mean") {
		t.Errorf("missing 'Did you mean' suggestion:\n%s", out)
	}
	if !strings.Contains(out, "optix analyze GOOGL --with-oi --expiry 2026-05-22") {
		t.Errorf("suggestion does not embed closest expiry:\n%s", out)
	}
}

func TestFormatExpiryErrorEmptyAvailable(t *testing.T) {
	out := FormatExpiryError("GOOGL", "20260523", nil, "optix analyze GOOGL --with-oi --expiry %s")
	if !strings.Contains(out, "no expirations available") {
		t.Errorf("expected explanatory message, got:\n%s", out)
	}
}

func TestFormatExpiryErrorBadRequestedDate(t *testing.T) {
	// Garbage requested string — still produce something useful (sort available chronologically).
	out := FormatExpiryError("GOOGL", "garbage", []string{"20260522", "20260520"}, "optix analyze GOOGL --with-oi --expiry %s")
	if !strings.Contains(out, "2026-05-20") || !strings.Contains(out, "2026-05-22") {
		t.Errorf("expected both expiries listed, got:\n%s", out)
	}
}
