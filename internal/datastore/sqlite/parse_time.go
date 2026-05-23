package sqlite

import (
	"log"
	"time"
)

// parseTimeOrLog parses a stored RFC3339 string. On parse failure (not on
// empty input — that legitimately represents "no value"), logs a warning
// and returns the zero time. Wraps the bare `t, _ := time.Parse(...)`
// pattern that pre-fix silently dropped read-path parse errors across
// GetAnalysisCache, GetBackgroundJob, GetBackgroundJobsForSymbol, and
// GetRecentFailures — see #44. The log catches writer/reader format drift
// the first time a stale row is read, instead of letting it cascade into
// zero-time-poisoned downstream queries (the trap #27 fell into).
func parseTimeOrLog(s, fieldCtx string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		log.Printf("sqlite: failed to parse %s timestamp %q: %v", fieldCtx, s, err)
	}
	return t
}
