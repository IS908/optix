package cli

import (
	"strings"
	"testing"
)

func TestPremarketCmd(t *testing.T) {
	cmd := newPremarketCmd()
	if cmd.Use != "premarket" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.Flags().Lookup("format") == nil {
		t.Error("missing --format")
	}
	if !strings.Contains(cmd.Long, "overnight") {
		t.Error("Long should describe the bundle")
	}
}
