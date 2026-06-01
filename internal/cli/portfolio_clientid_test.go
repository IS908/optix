package cli

import "testing"

// TestPortfolioClientIDDistinct locks the invariant that `optix portfolio`
// uses a ClientID distinct from every other subcommand. IBKR TWS rejects a
// second API client trying to connect with an already-in-use ClientID via
// error 326, so collisions silently turn into a confusing "connect to
// broker:" failure when two subcommands run concurrently (e.g. the
// portfolio cron overlapping with interactive `optix positions`). See
// issue #47 — the v0.5.0 release used 4 here and was vulnerable.
//
// The matrix mixes named consts (where they exist) with literals from
// commands that haven't extracted the value yet. Each literal carries a
// grep target so the next reviewer can confirm it still matches the
// source. If any of those subcommands later extracts its ClientID into a
// named const, swap the literal here for the const.
func TestPortfolioClientIDDistinct(t *testing.T) {
	matrix := map[string]int{
		"journal/trades/server (master)":                journalClientID, // const, value 0
		"max-pain":                                      maxPainClientID, // const, value 8
		"chain":                                         chainClientID,   // const, value 9
		"quote (literal, quote.go:33)":                  1,
		"analyze (literal, analyze.go:82)":              2,
		"dashboard (literal, dashboard.go:67)":          3,
		"positions (literal, positions.go:49)":          4,
		"analyze --watchlist (literal, analyze.go:214)": 6,
	}
	for name, id := range matrix {
		if portfolioClientID == id {
			t.Errorf("portfolioClientID = %d collides with %s (%d); pick a different slot or extract that command's ID into a named const so this test can reference it directly",
				portfolioClientID, name, id)
		}
	}
}
