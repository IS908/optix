package portfolio

import (
	"encoding/json"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// defaultSectorsJSON is the build-time-embedded fallback sector map. The
// canonical version of this file lives at configs/sectors.json in the repo
// root for human editing; internal/portfolio/default_sectors.json is a
// committed copy used by go:embed (which cannot traverse `..` or follow
// symlinks). A drift-detection test in embed_test.go fails if the two
// copies diverge — `make sync-sectors` (or just `cp configs/sectors.json
// internal/portfolio/default_sectors.json`) keeps them in lockstep.
//
//go:embed default_sectors.json
var defaultSectorsJSON []byte

// LoadDefaultSectorMap parses the embedded sector map. Returns a non-nil
// SectorMap on success. The embedded JSON is validated at process start; a
// parse failure here is a build-time mistake, not a runtime user error.
func LoadDefaultSectorMap() (*SectorMap, error) {
	var sm SectorMap
	if err := json.Unmarshal(defaultSectorsJSON, &sm); err != nil {
		return nil, fmt.Errorf("parse embedded sectors.json: %w", err)
	}
	if sm.SectorLabels == nil {
		sm.SectorLabels = map[string]string{}
	}
	if sm.TickerSectors == nil {
		sm.TickerSectors = map[string]string{}
	}
	return &sm, nil
}

// ResolveSectorMap returns the sector map a caller should use, following a
// search chain that prefers explicit overrides but always succeeds with the
// embedded fallback. The returned `source` describes which path was used,
// for surfacing to the user in --verbose / debug output.
//
// Search order, with the loud-vs-silent failure model for each tier:
//
//  1. explicit path arg            — LOUD on missing/malformed
//  2. $OPTIX_SECTORS_FILE          — LOUD on missing/malformed (env var is
//                                    explicit user intent, same class as the
//                                    flag; silently swallowing a typo would
//                                    mirror issue #48 one level down)
//  3. <executable-dir>/../configs/sectors.json  (release-bundle auto-discovery)
//  4. ./configs/sectors.json                    (dev-checkout auto-discovery)
//  5. embedded default                          (never empty)
//
// Auto-discovery tiers (3–4) silently skip missing files — that's the right
// behavior because they're "is the map here?" probes, not user assertions
// about where the map lives.
//
// For the executable-dir probe, symlinks are resolved first so that
// Homebrew-style installs (/opt/homebrew/bin/optix → ../Cellar/.../optix)
// look in the real bundle's configs/ rather than the formula prefix.
func ResolveSectorMap(explicit string) (sm *SectorMap, source string, err error) {
	if explicit != "" {
		loaded, e := LoadSectorMap(explicit)
		if e != nil {
			return nil, "", fmt.Errorf("load --sectors-file %q: %w", explicit, e)
		}
		return loaded, explicit, nil
	}

	// Tier 2: env var is user intent — fail loudly on a bad path rather than
	// silently degrading to the embedded fallback.
	if envPath := os.Getenv("OPTIX_SECTORS_FILE"); envPath != "" {
		loaded, e := LoadSectorMap(envPath)
		if e != nil {
			return nil, "", fmt.Errorf("load $OPTIX_SECTORS_FILE=%q: %w", envPath, e)
		}
		return loaded, envPath, nil
	}

	// Tiers 3–4: auto-discovery; missing files silently skip.
	for _, candidate := range autoSectorPaths() {
		if candidate == "" {
			continue
		}
		if _, statErr := os.Stat(candidate); statErr != nil {
			continue
		}
		loaded, e := LoadSectorMap(candidate)
		if e == nil {
			return loaded, candidate, nil
		}
		// File exists but failed to parse — surface that rather than silently
		// falling through to embedded. A malformed file on a discovered path
		// is still likely a user-edited customization gone wrong.
		return nil, "", fmt.Errorf("load %q (auto-discovered): %w", candidate, e)
	}

	loaded, e := LoadDefaultSectorMap()
	if e != nil {
		// Build-time bug — the embedded JSON should always parse.
		return nil, "", e
	}
	return loaded, "<embedded>", nil
}

func autoSectorPaths() []string {
	var out []string
	if exe, err := os.Executable(); err == nil {
		// Resolve symlinks so Homebrew-style installs land at the real
		// bundle's configs/ dir rather than the formula prefix. Falls
		// through cleanly when exe isn't a symlink.
		if real, evalErr := filepath.EvalSymlinks(exe); evalErr == nil {
			exe = real
		}
		out = append(out, filepath.Join(filepath.Dir(exe), "..", "configs", "sectors.json"))
	}
	out = append(out, "./configs/sectors.json")
	return out
}
