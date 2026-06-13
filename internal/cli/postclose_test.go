package cli

import (
	"strings"
	"testing"
)

func TestPostcloseCmd(t *testing.T) {
	cmd := newPostcloseCmd()
	if cmd.Use != "postclose" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.Flags().Lookup("format") == nil {
		t.Error("missing --format")
	}
	if !strings.Contains(cmd.Long, "earnings") {
		t.Error("Long should describe the bundle")
	}
}
