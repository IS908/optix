package cli

import (
	"errors"
	"fmt"
	"testing"
)

// TestAsExitCode covers the documented behavior of the journal-command exit
// translator: nil → 0, ExitErr (direct or wrapped) → its Code, anything else → 1.
func TestAsExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"plain error", errors.New("boom"), 1},
		{"ExitErr direct", &ExitErr{Err: errors.New("ibkr"), Code: 2}, 2},
		{"ExitErr wrapped", fmt.Errorf("ctx: %w", &ExitErr{Err: errors.New("sql"), Code: 3}), 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AsExitCode(tc.err); got != tc.want {
				t.Errorf("AsExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestExitErrUnwrap ensures errors.As / errors.Is keep working with wrapping.
func TestExitErrUnwrap(t *testing.T) {
	inner := errors.New("inner")
	wrapped := fmt.Errorf("outer: %w", &ExitErr{Err: inner, Code: 2})

	var ex *ExitErr
	if !errors.As(wrapped, &ex) {
		t.Fatalf("errors.As did not find ExitErr in wrapped chain")
	}
	if ex.Code != 2 {
		t.Errorf("Code = %d, want 2", ex.Code)
	}
	if !errors.Is(wrapped, inner) {
		t.Errorf("errors.Is did not reach the inner error through ExitErr.Unwrap")
	}
}
