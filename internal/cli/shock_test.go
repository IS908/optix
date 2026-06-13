package cli

import (
	"strings"
	"testing"
)

func TestShockCmd(t *testing.T) {
	cmd := newShockCmd()
	if cmd.Use != "shock" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.Flags().Lookup("format") == nil {
		t.Error("missing --format")
	}
	if !strings.Contains(cmd.Long, "regime") || !strings.Contains(cmd.Long, "liquidity") {
		t.Error("Long should describe shock regime and liquidity bundle")
	}
}
