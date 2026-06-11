package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// UpsertPulseBars 幂等写入 sparkline bar（INSERT OR REPLACE on (asset_id, ts)）。
func (s *Store) UpsertPulseBars(ctx context.Context, assetID string, bars []model.OHLCV) error {
	if len(bars) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.PrepareContext(ctx, `
        INSERT OR REPLACE INTO market_pulse_bars
            (asset_id, ts, open, high, low, close, volume)
        VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()
	for _, b := range bars {
		if _, err := stmt.ExecContext(ctx, assetID, b.Timestamp.UTC().Format(time.RFC3339),
			b.Open, b.High, b.Low, b.Close, b.Volume); err != nil {
			return fmt.Errorf("upsert pulse bar: %w", err)
		}
	}
	return tx.Commit()
}

// GetPulseBars 返回 since 之后的 bar，ts 升序。
func (s *Store) GetPulseBars(ctx context.Context, assetID string, since time.Time) ([]model.OHLCV, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT ts, open, high, low, close, volume FROM market_pulse_bars
        WHERE asset_id = ? AND ts > ? ORDER BY ts ASC`,
		assetID, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("query pulse bars: %w", err)
	}
	defer rows.Close()
	var out []model.OHLCV
	for rows.Next() {
		var tsStr string
		var b model.OHLCV
		if err := rows.Scan(&tsStr, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume); err != nil {
			return nil, fmt.Errorf("scan pulse bar: %w", err)
		}
		b.Timestamp = parseTimeOrLog(tsStr, "market_pulse_bars.ts")
		out = append(out, b)
	}
	return out, rows.Err()
}

// LastPulseBarTS 返回该资产最新 bar 时间；无数据返回零值（非错误）。
func (s *Store) LastPulseBarTS(ctx context.Context, assetID string) (time.Time, error) {
	var tsStr *string
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(ts) FROM market_pulse_bars WHERE asset_id = ?`, assetID).Scan(&tsStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("last pulse bar ts: %w", err)
	}
	if tsStr == nil {
		return time.Time{}, nil
	}
	return parseTimeOrLog(*tsStr, "market_pulse_bars.max_ts"), nil
}

// PrunePulseBars 删除早于 retention 的 bar，返回删除行数。
func (s *Store) PrunePulseBars(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-retention).Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM market_pulse_bars WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune pulse bars: %w", err)
	}
	return res.RowsAffected()
}
