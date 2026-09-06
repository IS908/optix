// Package datahome owns persistent storage location and explicit SQLite migration.
package datahome

import (
	"fmt"
	"os"
	"path/filepath"
)

type Options struct {
	Flag, Env, Config, Home, XDG, OS string
	Legacy                           []string
}
type Resolution struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

func Resolve(o Options) (Resolution, error) {
	for _, v := range []Resolution{{o.Flag, "flag"}, {o.Env, "environment"}, {o.Config, "config"}} {
		if v.Path != "" {
			return v, nil
		}
	}
	if o.Home == "" {
		return Resolution{}, fmt.Errorf("home directory unavailable; specify --db")
	}
	base := filepath.Join(o.Home, ".local", "share")
	if o.OS == "darwin" {
		base = filepath.Join(o.Home, "Library", "Application Support")
	}
	if o.XDG != "" {
		if !filepath.IsAbs(o.XDG) {
			return Resolution{}, fmt.Errorf("XDG_DATA_HOME must be absolute")
		}
		base = o.XDG
	}
	out := Resolution{filepath.Join(base, "optix", "optix.db"), "default"}
	// Even when the new path exists, require an explicit selection if legacy
	// data remains: never silently select one of two potentially divergent DBs.
	for _, p := range o.Legacy {
		abs, err := filepath.Abs(p)
		if err != nil {
			return Resolution{}, err
		}
		if abs == out.Path {
			continue
		}
		if _, err := os.Lstat(p); err == nil {
			return Resolution{}, fmt.Errorf("legacy database at %s; select it with --db or run optix data migrate --from %q --to %q (stop writers before switching)", p, p, out.Path)
		} else if !os.IsNotExist(err) {
			return Resolution{}, err
		}
	}
	return out, nil
}
