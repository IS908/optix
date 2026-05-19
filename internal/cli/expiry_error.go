package cli

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// FormatExpiryError builds the user-facing error message shown when a caller
// requested an expiry that the broker does not offer. The available list is
// sorted by absolute distance from the requested date and rendered as
// "YYYY-MM-DD (N days away)" rows, followed by a copy-paste-ready "Did you
// mean" suggestion built from suggestionTemplate (must contain a single %s
// for the closest expiry, formatted as YYYY-MM-DD).
//
// Inputs are YYYYMMDD strings (matching IBKR's native format). Output uses
// YYYY-MM-DD throughout. Bad inputs degrade gracefully: an unparseable
// requested string falls back to chronological sort; an empty available
// list prints a clear no-options-available message.
func FormatExpiryError(underlying, requested string, available []string, suggestionTemplate string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Error: no such expiry %s for %s\n", dashed(requested), underlying)

	if len(available) == 0 {
		fmt.Fprintln(&b, "\nno expirations available from broker")
		return b.String()
	}

	type entry struct {
		raw      string // YYYYMMDD
		parsed   time.Time
		distance int // absolute days from requested; math.MaxInt if either side unparseable
	}
	reqT, reqOK := parseCompactDate(requested)

	entries := make([]entry, 0, len(available))
	for _, a := range available {
		t, ok := parseCompactDate(a)
		dist := math.MaxInt
		if ok && reqOK {
			dist = absDays(t, reqT)
		}
		entries = append(entries, entry{raw: a, parsed: t, distance: dist})
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].distance != entries[j].distance {
			return entries[i].distance < entries[j].distance
		}
		// Tiebreak chronologically (older first feels stable).
		return entries[i].parsed.Before(entries[j].parsed)
	})

	fmt.Fprintln(&b, "\nAvailable (closest first):")
	for _, e := range entries {
		days := e.distance
		switch {
		case days == math.MaxInt:
			fmt.Fprintf(&b, "  %s\n", dashed(e.raw))
		case days == 1:
			fmt.Fprintf(&b, "  %s  (1 day away)\n", dashed(e.raw))
		default:
			fmt.Fprintf(&b, "  %s  (%d days away)\n", dashed(e.raw), days)
		}
	}

	closest := entries[0].raw
	fmt.Fprintf(&b, "\nDid you mean:\n  "+suggestionTemplate+"\n", dashed(closest))
	return b.String()
}

func parseCompactDate(s string) (time.Time, bool) {
	t, err := time.Parse("20060102", s)
	return t, err == nil
}

// dashed converts YYYYMMDD → YYYY-MM-DD. Falls back to the input on any
// length mismatch (callers may pass already-formatted strings).
func dashed(s string) string {
	if len(s) != 8 {
		return s
	}
	return s[:4] + "-" + s[4:6] + "-" + s[6:]
}

func absDays(a, b time.Time) int {
	d := a.Sub(b).Hours() / 24
	if d < 0 {
		d = -d
	}
	return int(d + 0.5)
}
