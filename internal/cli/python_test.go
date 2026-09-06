package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootUsesProjectPythonAndPreservesOverride(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	venv := filepath.Join(dir, "python", ".venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(venv), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(venv, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	root := NewRootCmd()
	got, _ := root.PersistentFlags().GetString("python")
	if got != venv {
		t.Fatalf("default Python = %q, want project venv %q", got, venv)
	}
	if err := root.ParseFlags([]string{"--python", "/custom/python"}); err != nil {
		t.Fatal(err)
	}
	got, _ = root.PersistentFlags().GetString("python")
	if got != "/custom/python" {
		t.Fatalf("explicit Python override lost: %q", got)
	}
}

func TestInstalledRuntimePythonWorksOutsideCheckoutAndThroughSymlink(t *testing.T) {
	runtimeDir := t.TempDir()
	venv := filepath.Join(runtimeDir, "python", ".venv", "bin", "python")
	binary := filepath.Join(runtimeDir, "bin", "optix")
	for _, path := range []string{venv, binary} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	other := t.TempDir()
	link := filepath.Join(other, "optix")
	if err := os.Symlink(binary, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{binary, link} {
		got := pythonForRuntime(path, other)
		actual, err := os.Stat(got)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.Stat(venv)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(actual, expected) {
			t.Fatalf("runtime Python from %q = %q", path, got)
		}
	}
	if err := os.Chmod(venv, 0644); err != nil {
		t.Fatal(err)
	}
	if got := pythonForRuntime(binary, other); got != "python3" {
		t.Fatalf("non-executable interpreter selected: %q", got)
	}
}
