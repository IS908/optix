package cli

import "testing"

// TestPortfolioClientIDDistinct locks the invariant that `optix portfolio`
// uses a ClientID distinct from `optix positions` (which is hard-coded to 4
// in positions.go). IBKR TWS rejects a second API client trying to connect
// with an already-in-use ClientID via error 326, so collisions silently
// turn into a confusing "connect to broker:" failure when cron-style usage
// overlaps with an interactive `optix positions`. See issue #47 — the v0.5.0
// release used 4 here and was vulnerable.
func TestPortfolioClientIDDistinct(t *testing.T) {
	const positionsClientID = 4 // matches the literal in positions.go:49
	if portfolioClientID == positionsClientID {
		t.Errorf("portfolioClientID = %d, must not collide with positionsClientID (%d); see issue #47",
			portfolioClientID, positionsClientID)
	}
	// Also verify we don't collide with the named-const slots in the matrix.
	for name, id := range map[string]int{
		"journal/trades/server (master)": journalClientID,
		"maxPain":                        maxPainClientID,
	} {
		if portfolioClientID == id {
			t.Errorf("portfolioClientID = %d collides with %s (%d)",
				portfolioClientID, name, id)
		}
	}
}
