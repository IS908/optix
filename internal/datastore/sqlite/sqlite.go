package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/IS908/optix/pkg/model"

	_ "modernc.org/sqlite"
)

//go:embed migrations/001_initial.sql
var migrationSQL string

// Store implements data persistence using SQLite.
type Store struct {
	db *sql.DB
}

// New opens (or creates) a SQLite database and runs migrations.
func New(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(migrationSQL)
	return err
}

// --- Stock Quotes ---

// UpsertStockQuote inserts or updates a stock quote.
func (s *Store) UpsertStockQuote(ctx context.Context, q *model.StockQuote) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO stock_quotes (symbol, last_price, bid, ask, volume, change_val, change_pct, high, low, open_price, close_price, high_52w, low_52w, avg_volume, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(symbol) DO UPDATE SET
			last_price=excluded.last_price, bid=excluded.bid, ask=excluded.ask,
			volume=excluded.volume, change_val=excluded.change_val, change_pct=excluded.change_pct,
			high=excluded.high, low=excluded.low, open_price=excluded.open_price, close_price=excluded.close_price,
			high_52w=excluded.high_52w, low_52w=excluded.low_52w, avg_volume=excluded.avg_volume,
			updated_at=excluded.updated_at`,
		q.Symbol, q.Last, q.Bid, q.Ask, q.Volume, q.Change, q.ChangePct,
		q.High, q.Low, q.Open, q.Close, q.High52W, q.Low52W, q.AvgVolume,
		q.Timestamp.Format(time.RFC3339),
	)
	return err
}

// GetStockQuote retrieves the latest cached quote.
func (s *Store) GetStockQuote(ctx context.Context, symbol string) (*model.StockQuote, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT symbol, last_price, bid, ask, volume, change_val, change_pct, high, low, open_price, close_price, high_52w, low_52w, avg_volume, updated_at
		FROM stock_quotes WHERE symbol = ?`, symbol)

	q := &model.StockQuote{}
	var ts string
	err := row.Scan(&q.Symbol, &q.Last, &q.Bid, &q.Ask, &q.Volume, &q.Change, &q.ChangePct,
		&q.High, &q.Low, &q.Open, &q.Close, &q.High52W, &q.Low52W, &q.AvgVolume, &ts)
	if err != nil {
		return nil, err
	}
	q.Timestamp, _ = time.Parse(time.RFC3339, ts)
	return q, nil
}

// --- OHLCV Bars ---

// InsertBars inserts historical bars (ignoring duplicates).
func (s *Store) InsertBars(ctx context.Context, symbol, timeframe string, bars []model.OHLCV) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO ohlcv_bars (symbol, timeframe, open_time, open, high, low, close, volume)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, b := range bars {
		_, err := stmt.ExecContext(ctx, symbol, timeframe, b.Timestamp.Format(time.RFC3339),
			b.Open, b.High, b.Low, b.Close, b.Volume)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetBars retrieves historical bars for a symbol.
func (s *Store) GetBars(ctx context.Context, symbol, timeframe string, limit int) ([]model.OHLCV, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT open_time, open, high, low, close, volume
		FROM ohlcv_bars
		WHERE symbol = ? AND timeframe = ?
		ORDER BY open_time DESC
		LIMIT ?`, symbol, timeframe, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bars []model.OHLCV
	for rows.Next() {
		var b model.OHLCV
		var ts string
		if err := rows.Scan(&ts, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume); err != nil {
			return nil, err
		}
		b.Timestamp, _ = time.Parse(time.RFC3339, ts)
		bars = append(bars, b)
	}
	return bars, rows.Err()
}

// --- Watchlist ---

// AddToWatchlist adds a symbol to the watchlist.
func (s *Store) AddToWatchlist(ctx context.Context, symbol string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO watchlist (symbol, added_at) VALUES (?, ?)`,
		symbol, time.Now().Format(time.RFC3339))
	return err
}

// RemoveFromWatchlist removes a symbol from the watchlist.
func (s *Store) RemoveFromWatchlist(ctx context.Context, symbol string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM watchlist WHERE symbol = ?`, symbol)
	return err
}

// SaveWatchlistSnapshot upserts a daily snapshot for a watchlist symbol.
func (s *Store) SaveWatchlistSnapshot(ctx context.Context, snap model.QuickSummary) error {
	date := time.Now().Format("2006-01-02")
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO watchlist_snapshots
			(symbol, snapshot_date, price, trend, rsi, iv_rank, max_pain, pcr,
			 range_low_1s, range_high_1s, recommendation, opportunity_score)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(symbol, snapshot_date) DO UPDATE SET
			price=excluded.price, trend=excluded.trend, rsi=excluded.rsi,
			iv_rank=excluded.iv_rank, max_pain=excluded.max_pain, pcr=excluded.pcr,
			range_low_1s=excluded.range_low_1s, range_high_1s=excluded.range_high_1s,
			recommendation=excluded.recommendation, opportunity_score=excluded.opportunity_score`,
		snap.Symbol, date, snap.Price, snap.Trend, snap.RSI, snap.IVRank,
		snap.MaxPain, snap.PCR, snap.RangeLow1S, snap.RangeHigh1S,
		snap.Recommendation, snap.OpportunityScore,
	)
	return err
}

// GetWatchlist returns all watchlist symbols.
func (s *Store) GetWatchlist(ctx context.Context) ([]model.WatchlistItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT symbol, added_at, notes, tags FROM watchlist ORDER BY added_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.WatchlistItem
	for rows.Next() {
		var item model.WatchlistItem
		var tags string
		if err := rows.Scan(&item.Symbol, &item.AddedAt, &item.Notes, &tags); err != nil {
			return nil, err
		}
		// tags is JSON, but we store as simple string for now
		items = append(items, item)
	}
	return items, rows.Err()
}
