package webui

// watchlistConfigUpdater is the slice of *sqlite.Store that applyWatchlistConfig
// actually needs — kept as a local interface so the helper is testable without
// constructing a real SQLite store.
type watchlistConfigUpdater interface {
	UpdateWatchlistConfig(symbol string, enabled bool, intervalMinutes int) error
}

// applyWatchlistConfig sets auto-refresh config for each symbol in turn,
// collecting per-symbol errors instead of failing the batch. Returns a map
// keyed by symbol so the caller can log each failure without aborting the
// rest of the batch.
//
// Replaces the bare `_ = err` swallow in handleWatchlistAdd that contradicted
// its own "Log error but don't fail" comment — see #37.
func applyWatchlistConfig(store watchlistConfigUpdater, symbols []string, autoRefresh bool, intervalMinutes int) map[string]error {
	errs := make(map[string]error)
	for _, sym := range symbols {
		if err := store.UpdateWatchlistConfig(sym, autoRefresh, intervalMinutes); err != nil {
			errs[sym] = err
		}
	}
	return errs
}
