package cli

import "testing"

// TestExecutionsReaderClientIDsAreMaster locks the invariant that both CLI
// subcommands which read fills via IBKR's `reqExecutions` (`optix journal`
// and `optix trades`) use ClientID 0 — the IBKR implicit master.
//
// IBKR's `reqExecutions` returns only the connecting client's own fills
// unless the connecting ClientID matches TWS's "Master API Client ID"
// setting. Since optix is read-only and never places orders, a non-zero
// ClientID here would silently return 0 fills regardless of how much the
// user trades in TWS. ClientID 0 is IBKR's implicit master and gets
// cross-client execution visibility without requiring users to configure
// anything in TWS. See issue #30.
//
// Other subcommands (quote, analyze, dashboard, positions, max-pain) use
// distinct non-zero ClientIDs because they call account/market APIs
// (`reqMktData`, `reqPositions`, etc.) that are not gated on master
// status. Those IDs MUST stay non-zero to avoid colliding with the
// executions-reader path.
func TestExecutionsReaderClientIDsAreMaster(t *testing.T) {
	if journalClientID != 0 {
		t.Errorf("journalClientID = %d, want 0 (IBKR implicit master); see issue #30",
			journalClientID)
	}
	if tradesClientID != 0 {
		t.Errorf("tradesClientID = %d, want 0 (IBKR implicit master); see issue #30",
			tradesClientID)
	}
}
