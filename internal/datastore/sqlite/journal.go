package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// JournalFilter narrows ListExecutions. Zero-valued fields are no-ops.
type JournalFilter struct {
	Symbol  string
	Side    string // BOT | SLD
	SecType string // STK | OPT
	Account string
	Since   time.Time
	Until   time.Time
	Limit   int // 0 = no limit
}

// UpsertExecutions inserts each execution with INSERT OR IGNORE keyed on
// exec_id UNIQUE. Returns the count of NEWLY inserted rows. Safe to call
// repeatedly with the same input.
func (s *Store) UpsertExecutions(ctx context.Context, execs []model.Execution) (int, error) {
	if len(execs) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.PrepareContext(ctx, `
        INSERT OR IGNORE INTO trade_journal
            (exec_id, time, account, symbol, sec_type, expiration, strike, right,
             side, quantity, price, avg_price, exchange, order_id, perm_id, synced_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	newCount := 0
	for _, e := range execs {
		res, err := stmt.ExecContext(ctx,
			e.ExecID, e.Time.UTC().Format(time.RFC3339), e.Account, e.Symbol,
			e.SecType, e.Expiration, e.Strike, e.Right,
			e.Side, e.Shares, e.Price, e.AvgPrice, e.Exchange, e.OrderID, e.PermID, now)
		if err != nil {
			return 0, fmt.Errorf("insert exec %s: %w", e.ExecID, err)
		}
		affected, _ := res.RowsAffected()
		newCount += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return newCount, nil
}

// ListExecutions returns executions matching the filter, ORDER BY time DESC.
func (s *Store) ListExecutions(ctx context.Context, f JournalFilter) ([]model.Execution, error) {
	var where []string
	var args []any
	if f.Symbol != "" {
		where = append(where, "symbol = ?")
		args = append(args, strings.ToUpper(f.Symbol))
	}
	if f.Side != "" {
		where = append(where, "side = ?")
		args = append(args, strings.ToUpper(f.Side))
	}
	if f.SecType != "" {
		where = append(where, "sec_type = ?")
		args = append(args, strings.ToUpper(f.SecType))
	}
	if f.Account != "" {
		where = append(where, "account = ?")
		args = append(args, f.Account)
	}
	if !f.Since.IsZero() {
		where = append(where, "time >= ?")
		args = append(args, f.Since.UTC().Format(time.RFC3339))
	}
	if !f.Until.IsZero() {
		where = append(where, "time <= ?")
		args = append(args, f.Until.UTC().Format(time.RFC3339))
	}
	q := `SELECT exec_id, time, account, symbol, sec_type, expiration, strike, right,
                 side, quantity, price, avg_price, exchange, order_id, perm_id
            FROM trade_journal`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY time DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var out []model.Execution
	for rows.Next() {
		var e model.Execution
		var ts string
		if err := rows.Scan(&e.ExecID, &ts, &e.Account, &e.Symbol, &e.SecType,
			&e.Expiration, &e.Strike, &e.Right, &e.Side, &e.Shares, &e.Price,
			&e.AvgPrice, &e.Exchange, &e.OrderID, &e.PermID); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.Time = t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SyncState reflects the single-row trade_journal_sync_state table.
// A zero LastSyncAt means "never synced" — the bootstrap row.
type SyncState struct {
	LastSyncAt    time.Time
	LastSyncCount int
	LastError     string
}

// GetSyncState returns the single-row sync state.
func (s *Store) GetSyncState(ctx context.Context) (SyncState, error) {
	var st SyncState
	var ts string
	err := s.db.QueryRowContext(ctx,
		`SELECT last_sync_at, last_sync_count, last_error
           FROM trade_journal_sync_state WHERE id = 1`,
	).Scan(&ts, &st.LastSyncCount, &st.LastError)
	if err != nil {
		return st, fmt.Errorf("get sync state: %w", err)
	}
	if ts != "" {
		if t, perr := time.Parse(time.RFC3339, ts); perr == nil {
			st.LastSyncAt = t
		}
	}
	return st, nil
}

// UpdateSyncState writes the single-row sync state.
func (s *Store) UpdateSyncState(ctx context.Context, st SyncState) error {
	ts := ""
	if !st.LastSyncAt.IsZero() {
		ts = st.LastSyncAt.UTC().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE trade_journal_sync_state
            SET last_sync_at = ?, last_sync_count = ?, last_error = ?
          WHERE id = 1`,
		ts, st.LastSyncCount, st.LastError)
	if err != nil {
		return fmt.Errorf("update sync state: %w", err)
	}
	return nil
}
