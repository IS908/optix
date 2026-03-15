package watchlist

import (
	"context"
	"fmt"
	"strings"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/pkg/model"
)

// Manager handles watchlist operations.
type Manager struct {
	store *sqlite.Store
}

// NewManager creates a watchlist manager.
func NewManager(store *sqlite.Store) *Manager {
	return &Manager{store: store}
}

// Add adds one or more symbols to the watchlist.
func (m *Manager) Add(ctx context.Context, symbols ...string) error {
	for _, s := range symbols {
		sym := strings.ToUpper(strings.TrimSpace(s))
		if sym == "" {
			continue
		}
		if err := m.store.AddToWatchlist(ctx, sym); err != nil {
			return fmt.Errorf("add %s: %w", sym, err)
		}
	}
	return nil
}

// Remove removes a symbol from the watchlist.
func (m *Manager) Remove(ctx context.Context, symbol string) error {
	return m.store.RemoveFromWatchlist(ctx, strings.ToUpper(strings.TrimSpace(symbol)))
}

// List returns all watchlist items.
func (m *Manager) List(ctx context.Context) ([]model.WatchlistItem, error) {
	return m.store.GetWatchlist(ctx)
}

// Symbols returns just the symbol strings.
func (m *Manager) Symbols(ctx context.Context) ([]string, error) {
	items, err := m.store.GetWatchlist(ctx)
	if err != nil {
		return nil, err
	}
	syms := make([]string, len(items))
	for i, item := range items {
		syms[i] = item.Symbol
	}
	return syms, nil
}
