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
// Search order:
//  1. explicit path (the value passed to this function), if non-empty
//  2. $OPTIX_SECTORS_FILE
//  3. <executable-dir>/../configs/sectors.json  (release-bundle layout)
//  4. ./configs/sectors.json                    (dev-checkout fallback)
//  5. embedded default                          (never empty)
//
// Returns an error only when an explicit override exists but fails to load —
// silently falling back from a user's typo would be worse than failing
// loudly. Missing files in the auto-search chain (2–4) are treated as
// "this path isn't where the map lives" rather than errors.
func ResolveSectorMap(explicit string) (sm *SectorMap, source string, err error) {
	if explicit != "" {
		loaded, e := LoadSectorMap(explicit)
		if e != nil {
			return nil, "", fmt.Errorf("load --sectors-file %q: %w", explicit, e)
		}
		return loaded, explicit, nil
	}

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
		// falling through to embedded. A malformed user-edited override
		// should not be hidden.
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
	out := []string{
		os.Getenv("OPTIX_SECTORS_FILE"),
	}
	if exe, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(exe), "..", "configs", "sectors.json"))
	}
	out = append(out, "./configs/sectors.json")
	return out
}
