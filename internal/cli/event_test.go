package cli

import (
	"strings"
	"testing"
)

func TestEventCmd(t *testing.T) {
	cmd := newEventCmd()
	if cmd.Use != "event" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.Flags().Lookup("format") == nil {
		t.Error("missing --format")
	}
	if !strings.Contains(cmd.Long, "FOMC") || !strings.Contains(cmd.Long, "CPI") {
		t.Error("Long should describe FOMC/CPI event bundle")
	}
}
