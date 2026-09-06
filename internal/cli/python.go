package cli

import (
	"os"
	"path/filepath"
)

// defaultPython uses the runtime installed alongside bin/optix, or the
// source checkout's venv for go run. Explicit --python flags still win.
func defaultPython() string {
	executable, _ := os.Executable()
	cwd, _ := os.Getwd()
	return pythonForRuntime(executable, cwd)
}

func pythonForRuntime(executable, cwd string) string {
	var roots []string
	if executable != "" {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		roots = append(roots, filepath.Dir(filepath.Dir(executable)))
	}
	if cwd != "" {
		roots = append(roots, cwd)
	}
	for _, root := range roots {
		candidate := filepath.Join(root, "python", ".venv", "bin", "python")
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return candidate
		}
	}
	return "python3"
}
