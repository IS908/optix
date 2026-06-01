package sqlite

import (
	"context"
	"testing"
)

func TestInMemoryStoreSharesSchemaAcrossConnectionPool(t *testing.T) {
	ctx := context.Background()

	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	conn, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve first connection: %v", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT COUNT(*) FROM watchlist`); err != nil {
		t.Fatalf("schema missing on reserved connection: %v", err)
	}

	if err := s.AddToWatchlist(ctx, "AAPL"); err != nil {
		t.Fatalf("schema should be visible to pooled connection: %v", err)
	}
}
