package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/IS908/optix/pkg/model"
)

const scanCandidateCols = `candidate_id, scan_date, rank, symbol, right, expiry, dte,
strike, spot, bid, ask, mid, iv, delta, oi, volume,
cushion_pct, premium_yield_pct, annualized_yield_pct, score,
ibkr_bid, ibkr_ask, ibkr_option_iv, ibkr_option_delta, symbol_source, created_at`

// InsertScanCandidates 单事务整批插入；UNIQUE 冲突逐条跳过计入 skipped。
func (s *Store) InsertScanCandidates(ctx context.Context, cands []model.ScanCandidate) (int, int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO scan_candidates (`+scanCandidateCols+`)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(scan_date, symbol, expiry, strike) DO NOTHING`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()
	registered, skipped := 0, 0
	for _, c := range cands {
		res, err := stmt.ExecContext(ctx,
			c.CandidateID, c.ScanDate, c.Rank, c.Symbol, c.Right, c.Expiry, c.DTE,
			c.Strike, c.Spot, c.Bid, nullF(c.Ask), nullF(c.Mid), nullF(c.IV), nullF(c.Delta),
			nullI(c.OI), nullI(c.Volume),
			c.CushionPct, c.PremiumYieldPct, c.AnnualizedYieldPct, c.Score,
			nullF(c.IBKRBid), nullF(c.IBKRAsk), nullF(c.IBKROptionIV), nullF(c.IBKROptionDelta),
			c.SymbolSource, c.CreatedAt.UTC().Format(time.RFC3339))
		if err != nil {
			return 0, 0, fmt.Errorf("insert %s: %w", c.CandidateID, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			registered++
		} else {
			skipped++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit: %w", err)
	}
	return registered, skipped, nil
}

func nullF(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullI(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func (s *Store) scanScanCandidates(rows *sql.Rows) ([]model.ScanCandidate, error) {
	out := []model.ScanCandidate{}
	for rows.Next() {
		var c model.ScanCandidate
		var ask, mid, iv, delta, ibid, iask, iiv, idelta sql.NullFloat64
		var oi, volume sql.NullInt64
		var createdAt string
		if err := rows.Scan(&c.CandidateID, &c.ScanDate, &c.Rank, &c.Symbol, &c.Right,
			&c.Expiry, &c.DTE, &c.Strike, &c.Spot, &c.Bid, &ask, &mid, &iv, &delta,
			&oi, &volume, &c.CushionPct, &c.PremiumYieldPct, &c.AnnualizedYieldPct,
			&c.Score, &ibid, &iask, &iiv, &idelta, &c.SymbolSource, &createdAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		c.Ask, c.Mid, c.IV, c.Delta = fromNullF(ask), fromNullF(mid), fromNullF(iv), fromNullF(delta)
		c.OI, c.Volume = fromNullI(oi), fromNullI(volume)
		c.IBKRBid, c.IBKRAsk, c.IBKROptionIV, c.IBKROptionDelta = fromNullF(ibid), fromNullF(iask), fromNullF(iiv), fromNullF(idelta)
		c.CreatedAt = parseTimeOrLog(createdAt, "scan_candidates.created_at")
		out = append(out, c)
	}
	return out, rows.Err()
}

func fromNullF(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func fromNullI(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}

// ExpiredUnsettledScanCandidates 返回 expiry < beforeDate 且尚无对账记录的候选。
func (s *Store) ExpiredUnsettledScanCandidates(ctx context.Context, beforeDate string) ([]model.ScanCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+scanCandidateCols+` FROM scan_candidates
        WHERE expiry < ? AND candidate_id NOT IN (SELECT candidate_id FROM scan_reconciliations)
        ORDER BY expiry, symbol`, beforeDate)
	if err != nil {
		return nil, fmt.Errorf("query expired: %w", err)
	}
	defer rows.Close()
	return s.scanScanCandidates(rows)
}

func (s *Store) InsertScanReconciliation(ctx context.Context, r model.ScanReconciliation) error {
	touched := 0
	if r.Touched {
		touched = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO scan_reconciliations
        (candidate_id, expiry_close, outcome, realized_pnl, touched, max_breach_pct, expiry_basis, settled_at)
        VALUES (?,?,?,?,?,?,?,?)`,
		r.CandidateID, r.ExpiryClose, r.Outcome, r.RealizedPnL, touched,
		r.MaxBreachPct, r.ExpiryBasis, r.SettledAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert reconciliation %s: %w", r.CandidateID, err)
	}
	return nil
}

// ListScanCandidatesSince fromDate=="" 返回全量，否则 scan_date >= fromDate。
func (s *Store) ListScanCandidatesSince(ctx context.Context, fromDate string) ([]model.ScanCandidate, error) {
	q := `SELECT ` + scanCandidateCols + ` FROM scan_candidates`
	args := []any{}
	if fromDate != "" {
		q += ` WHERE scan_date >= ?`
		args = append(args, fromDate)
	}
	q += ` ORDER BY scan_date, rank`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()
	return s.scanScanCandidates(rows)
}

func (s *Store) ListScanReconciliations(ctx context.Context, candidateIDs []string) (map[string]model.ScanReconciliation, error) {
	out := map[string]model.ScanReconciliation{}
	if len(candidateIDs) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(candidateIDs)), ",")
	args := make([]any, len(candidateIDs))
	for i, id := range candidateIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `SELECT candidate_id, expiry_close, outcome, realized_pnl,
        touched, max_breach_pct, expiry_basis, settled_at
        FROM scan_reconciliations WHERE candidate_id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query reconciliations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r model.ScanReconciliation
		var touched int
		var settledAt string
		if err := rows.Scan(&r.CandidateID, &r.ExpiryClose, &r.Outcome, &r.RealizedPnL,
			&touched, &r.MaxBreachPct, &r.ExpiryBasis, &settledAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		r.Touched = touched != 0
		r.SettledAt = parseTimeOrLog(settledAt, "scan_reconciliations.settled_at")
		out[r.CandidateID] = r
	}
	return out, rows.Err()
}
