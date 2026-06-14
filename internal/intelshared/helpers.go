package intelshared

import (
	"strings"
	"time"
	"unicode"
)

var nyLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.FixedZone("EST", -5*3600)
	}
	return loc
}()

// NY returns the America/New_York market-clock location.
func NY() *time.Location { return nyLoc }

// NormalizeSymbol canonicalizes user-entered ticker symbols for Market Intel universes.
func NormalizeSymbol(symbol string) string {
	var b strings.Builder
	b.Grow(len(symbol))
	for _, r := range symbol {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}
