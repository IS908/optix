package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/IS908/optix/pkg/model"
)

// UpsertGapStats 刷新整组跳空统计（INSERT OR REPLACE on UNIQUE(symbol,direction,band)）。
func (s *Store) UpsertGapStats(ctx context.Context, stats []model.PremarketGapStat) error {
	if len(stats) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.PrepareContext(ctx, `
        INSERT OR REPLACE INTO premarket_gap_stats
            (symbol, direction, band, fill_rate, sample_n, lookback_days, as_of)
        VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()
	for _, g := range stats {
		if _, err := stmt.ExecContext(ctx, g.Symbol, g.Direction, g.Band, g.FillRate,
			g.SampleN, g.LookbackDays, g.AsOf.UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("upsert gap stat: %w", err)
		}
	}
	return tx.Commit()
}

// GetGapStats 返回某标的全部跳空统计 + 最旧 as_of（判 TTL）；无数据 → (nil, zero, nil)。
func (s *Store) GetGapStats(ctx context.Context, symbol string) ([]model.PremarketGapStat, time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT symbol, direction, band, fill_rate, sample_n, lookback_days, as_of
        FROM premarket_gap_stats WHERE symbol = ? ORDER BY direction, band`, symbol)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("query gap stats: %w", err)
	}
	defer rows.Close()
	var out []model.PremarketGapStat
	var oldest time.Time
	for rows.Next() {
		var g model.PremarketGapStat
		var ts string
		if err := rows.Scan(&g.Symbol, &g.Direction, &g.Band, &g.FillRate,
			&g.SampleN, &g.LookbackDays, &ts); err != nil {
			return nil, time.Time{}, fmt.Errorf("scan gap stat: %w", err)
		}
		g.AsOf = parseTimeOrLog(ts, "premarket_gap_stats.as_of")
		if oldest.IsZero() || g.AsOf.Before(oldest) {
			oldest = g.AsOf
		}
		out = append(out, g)
	}
	return out, oldest, rows.Err()
}
