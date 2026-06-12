package cli

import (
	"testing"
)

func TestNewPulseCmdFlags(t *testing.T) {
	cmd := newPulseCmd()
	if got, _ := cmd.Flags().GetString("view"); got != "" {
		t.Errorf("default --view = %q, want \"\" (auto-infer)", got)
	}
	for _, name := range []string{"format", "with-sparkline", "strict"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not defined", name)
		}
	}
}
