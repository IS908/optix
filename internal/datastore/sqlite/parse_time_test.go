package sqlite

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"
)

// TestParseTimeOrLog pins the contract for #44: parse failures must be logged
// so format drift between writers and readers surfaces in operator logs
// instead of silently emitting zero times that downstream code mistakes for
// "no value".
func TestParseTimeOrLog(t *testing.T) {
	t.Run("valid RFC3339 parses correctly", func(t *testing.T) {
		captureLog(t)
		got := parseTimeOrLog("2026-05-23T14:30:00Z", "test.field")
		want := time.Date(2026, 5, 23, 14, 30, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("parseTimeOrLog = %v, want %v", got, want)
		}
	})

	t.Run("empty string returns zero without log", func(t *testing.T) {
		buf := captureLog(t)
		got := parseTimeOrLog("", "test.field")
		if !got.IsZero() {
			t.Errorf("empty: got %v, want zero", got)
		}
		if buf.Len() != 0 {
			t.Errorf("empty should not log; got %q", buf.String())
		}
	})

	t.Run("malformed input returns zero AND logs", func(t *testing.T) {
		buf := captureLog(t)
		got := parseTimeOrLog("not-a-timestamp", "test.field")
		if !got.IsZero() {
			t.Errorf("malformed: got %v, want zero", got)
		}
		out := buf.String()
		if !strings.Contains(out, "test.field") {
			t.Errorf("log should mention field context; got %q", out)
		}
		if !strings.Contains(out, "not-a-timestamp") {
			t.Errorf("log should include the offending value; got %q", out)
		}
	})

	t.Run("invalid date components fail parsing AND log", func(t *testing.T) {
		// A semantically impossible date (month 13) is the realistic drift
		// scenario — a future writer that doesn't validate before writing
		// could persist garbage; the read-side log surfaces it immediately.
		buf := captureLog(t)
		got := parseTimeOrLog("2026-13-99T25:99:99Z", "test.field")
		if !got.IsZero() {
			t.Errorf("invalid date: got %v, want zero", got)
		}
		if !strings.Contains(buf.String(), "failed to parse") {
			t.Errorf("expected parse-failure log; got %q", buf.String())
		}
	})
}

// captureLog redirects the default logger's output into a buffer for the
// duration of the test, restoring the original sink in cleanup. Returns the
// buffer the caller can inspect.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	return &buf
}
