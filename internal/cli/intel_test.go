package cli

import (
	"strings"
	"testing"
)

func TestIntelCmdHasSubcommands(t *testing.T) {
	cmd := newIntelCmd()
	want := map[string]bool{"status": true, "read": true, "narrative": true, "judge": true, "reconcile": true}
	for _, c := range cmd.Commands() {
		delete(want, c.Name())
	}
	if len(want) != 0 {
		t.Errorf("missing intel subcommands: %v", want)
	}
}

func TestEmitJSONOrText(t *testing.T) {
	// json 路径产出有效 JSON（这里只断言不 panic + 文本路径调用 textFn）
	called := false
	if err := emitJSONOrText("text", map[string]int{"a": 1}, func() { called = true }); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("text format should invoke textFn")
	}
}

func TestIntelJudgeFlags(t *testing.T) {
	cmd := newIntelJudgeCmd()
	for _, f := range []string{"asset", "direction", "threshold", "confidence", "expiry", "rationale", "supersedes", "format"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("judge missing --%s", f)
		}
	}
	if !strings.Contains(cmd.Short, "judgment") {
		t.Errorf("judge Short = %q", cmd.Short)
	}
}
