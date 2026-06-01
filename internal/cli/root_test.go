package cli

import (
	"os"
	"syscall"
	"testing"
)

func TestSignalExitCodePreservesInterruptedStatus(t *testing.T) {
	cases := []struct {
		sig  os.Signal
		want int
	}{
		{syscall.SIGINT, 130},
		{syscall.SIGTERM, 143},
	}
	for _, tc := range cases {
		if got := signalExitCode(tc.sig); got != tc.want {
			t.Fatalf("signalExitCode(%v) = %d, want %d", tc.sig, got, tc.want)
		}
	}
}
