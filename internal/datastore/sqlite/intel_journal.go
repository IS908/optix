package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// InsertIntelNarrative append-only 写叙事（INSERT OR IGNORE：重复 entry_id 静默跳过）。
func (s *Store) InsertIntelNarrative(ctx context.Context, n model.IntelNarrative) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT OR IGNORE INTO intel_narratives
            (entry_id, trading_date, checkpoint, phase, body, created_at)
        VALUES (?, ?, ?, ?, ?, ?)`,
		n.EntryID, n.TradingDate, n.Checkpoint, n.Phase, n.Body,
		n.CreatedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert intel narrative: %w", err)
	}
	return nil
}

// ListIntelNarratives 返回某交易日全部叙事条目，created_at 升序（含历史版）。
func (s *Store) ListIntelNarratives(ctx context.Context, tradingDate string) ([]model.IntelNarrative, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT entry_id, trading_date, checkpoint, phase, body, created_at
        FROM intel_narratives WHERE trading_date = ? ORDER BY created_at ASC`, tradingDate)
	if err != nil {
		return nil, fmt.Errorf("list intel narratives: %w", err)
	}
	defer rows.Close()
	var out []model.IntelNarrative
	for rows.Next() {
		var n model.IntelNarrative
		var ts string
		if err := rows.Scan(&n.EntryID, &n.TradingDate, &n.Checkpoint, &n.Phase, &n.Body, &ts); err != nil {
			return nil, fmt.Errorf("scan narrative: %w", err)
		}
		n.CreatedAt = parseTimeOrLog(ts, "intel_narratives.created_at")
		out = append(out, n)
	}
	return out, rows.Err()
}

// InsertIntelJudgment append-only 写判断。
func (s *Store) InsertIntelJudgment(ctx context.Context, j model.IntelJudgment) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT OR IGNORE INTO intel_judgments
            (judgment_id, trading_date, checkpoint, asset_id, asset_class, direction,
             threshold_pct, confidence, expiry_checkpoint, expiry_at, registered_price,
             registered_basis, rationale, supersedes, created_at)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.JudgmentID, j.TradingDate, j.Checkpoint, j.AssetID, j.AssetClass, j.Direction,
		j.ThresholdPct, j.Confidence, j.ExpiryCheckpoint, j.ExpiryAt.UTC().Format(time.RFC3339),
		j.RegisteredPrice, j.RegisteredBasis, nullStr(j.Rationale), nullStr(j.Supersedes),
		j.CreatedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert intel judgment: %w", err)
	}
	return nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) scanJudgments(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]model.IntelJudgment, error) {
	var out []model.IntelJudgment
	for rows.Next() {
		var j model.IntelJudgment
		var expiryAt, createdAt string
		var rationale, supersedes *string
		if err := rows.Scan(&j.JudgmentID, &j.TradingDate, &j.Checkpoint, &j.AssetID,
			&j.AssetClass, &j.Direction, &j.ThresholdPct, &j.Confidence, &j.ExpiryCheckpoint,
			&expiryAt, &j.RegisteredPrice, &j.RegisteredBasis, &rationale, &supersedes,
			&createdAt); err != nil {
			return nil, fmt.Errorf("scan judgment: %w", err)
		}
		j.ExpiryAt = parseTimeOrLog(expiryAt, "intel_judgments.expiry_at")
		j.CreatedAt = parseTimeOrLog(createdAt, "intel_judgments.created_at")
		if rationale != nil {
			j.Rationale = *rationale
		}
		if supersedes != nil {
			j.Supersedes = *supersedes
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

const judgmentCols = `judgment_id, trading_date, checkpoint, asset_id, asset_class, direction,
    threshold_pct, confidence, expiry_checkpoint, expiry_at, registered_price,
    registered_basis, rationale, supersedes, created_at`

// ListIntelJudgments 返回某交易日全部判断，created_at 升序。
func (s *Store) ListIntelJudgments(ctx context.Context, tradingDate string) ([]model.IntelJudgment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+judgmentCols+`
        FROM intel_judgments WHERE trading_date = ? ORDER BY created_at ASC`, tradingDate)
	if err != nil {
		return nil, fmt.Errorf("list intel judgments: %w", err)
	}
	defer rows.Close()
	return s.scanJudgments(rows)
}

// ExpiredUnsettledJudgments 返回 expiry_at ≤ asOf 且无结算行的判断（reconcile 用）。
func (s *Store) ExpiredUnsettledJudgments(ctx context.Context, asOf time.Time) ([]model.IntelJudgment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+judgmentCols+`
        FROM intel_judgments j
        WHERE j.expiry_at <= ?
          AND NOT EXISTS (SELECT 1 FROM intel_reconciliations r WHERE r.judgment_id = j.judgment_id)
        ORDER BY j.expiry_at ASC`, asOf.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("expired unsettled judgments: %w", err)
	}
	defer rows.Close()
	return s.scanJudgments(rows)
}

// InsertIntelReconciliation 写结算（INSERT OR IGNORE：幂等，已结算跳过）。
func (s *Store) InsertIntelReconciliation(ctx context.Context, r model.IntelReconciliation) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT OR IGNORE INTO intel_reconciliations
            (judgment_id, expiry_price, expiry_basis, outcome, delta_pct, settled_at)
        VALUES (?, ?, ?, ?, ?, ?)`,
		r.JudgmentID, r.ExpiryPrice, r.ExpiryBasis, r.Outcome, r.DeltaPct,
		r.SettledAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert intel reconciliation: %w", err)
	}
	return nil
}

// ListIntelReconciliations 按 judgment_id 批量取结算，返回 map[judgment_id]。
func (s *Store) ListIntelReconciliations(ctx context.Context, judgmentIDs []string) (map[string]model.IntelReconciliation, error) {
	out := map[string]model.IntelReconciliation{}
	for _, id := range judgmentIDs {
		var r model.IntelReconciliation
		var ts string
		err := s.db.QueryRowContext(ctx, `
            SELECT judgment_id, expiry_price, expiry_basis, outcome, delta_pct, settled_at
            FROM intel_reconciliations WHERE judgment_id = ?`, id).
			Scan(&r.JudgmentID, &r.ExpiryPrice, &r.ExpiryBasis, &r.Outcome, &r.DeltaPct, &ts)
		if err != nil {
			continue // 无结算行 → 跳过（判断仍待结算）
		}
		r.SettledAt = parseTimeOrLog(ts, "intel_reconciliations.settled_at")
		out[id] = r
	}
	return out, nil
}

// PulseCloseNear 返回 asset 在 [at-tolerance, at] 内最近（最大 ts ≤ at）bar 的收盘价。
// reconcile 取到期价用；无 bar → ok=false（调用方判 void）。
func (s *Store) PulseCloseNear(ctx context.Context, assetID string, at time.Time, tolerance time.Duration) (float64, time.Time, bool, error) {
	var close float64
	var tsStr string
	err := s.db.QueryRowContext(ctx, `
        SELECT close, ts FROM market_pulse_bars
        WHERE asset_id = ? AND ts <= ? AND ts >= ?
        ORDER BY ts DESC LIMIT 1`,
		assetID, at.UTC().Format(time.RFC3339),
		at.Add(-tolerance).UTC().Format(time.RFC3339)).Scan(&close, &tsStr)
	if err != nil {
		return 0, time.Time{}, false, nil // sql.ErrNoRows 或其它 → ok=false（不区分，缺价即 void）
	}
	return close, parseTimeOrLog(tsStr, "market_pulse_bars.ts"), true, nil
}
