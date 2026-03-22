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
var migration001SQL string

//go:embed migrations/002_background_refresh.sql
var migration002SQL string

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

	// Encode PRAGMAs in the DSN so that every connection opened by the
	// database/sql connection pool inherits them — not just the first one.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite only supports a single concurrent writer.  Limiting the pool
	// to 2 connections (1 writer + 1 reader) avoids most SQLITE_BUSY errors
	// while still allowing reads during writes (WAL mode).
	db.SetMaxOpenConns(2)

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
	// Migration 001: Initial schema
	if _, err := s.db.Exec(migration001SQL); err != nil {
		return fmt.Errorf("migration 001: %w", err)
	}

	// Migration 002: Background refresh system (idempotent)
	if err := s.migrate002(); err != nil {
		return fmt.Errorf("migration 002: %w", err)
	}

	// Idempotent schema additions — error is swallowed when column already exists.
	_, _ = s.db.Exec(`ALTER TABLE watchlist_snapshots ADD COLUMN last_refreshed_at TEXT`)
	return nil
}

// migrate002 applies migration 002 idempotently by checking for existing columns first.
func (s *Store) migrate002() error {
	// Add watchlist columns only if they don't exist
	if err := s.addColumnIfNotExists("watchlist", "auto_refresh_enabled", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfNotExists("watchlist", "refresh_interval_minutes", "INTEGER DEFAULT 15"); err != nil {
		return err
	}
	if err := s.addColumnIfNotExists("watchlist", "last_refreshed_at", "TEXT"); err != nil {
		return err
	}

	// Execute the rest of the migration (indexes and tables use IF NOT EXISTS)
	if _, err := s.db.Exec(migration002SQL); err != nil {
		return err
	}

	return nil
}

// addColumnIfNotExists adds a column to a table only if it doesn't already exist.
func (s *Store) addColumnIfNotExists(table, column, columnDef string) error {
	// Check if column exists by querying pragma_table_info
	var exists bool
	row := s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) > 0 FROM pragma_table_info('%s') WHERE name = ?", table), column)
	if err := row.Scan(&exists); err != nil {
		return fmt.Errorf("check column %s.%s: %w", table, column, err)
	}

	if !exists {
		query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, columnDef)
		if _, err := s.db.Exec(query); err != nil {
			return fmt.Errorf("add column %s.%s: %w", table, column, err)
		}
	}

	return nil
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
		q.Timestamp.UTC().Format(time.RFC3339),
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
// Timestamps are normalized to UTC before storage so that the same bar
// received with different timezone offsets (e.g. +08:00 vs -04:00) is
// correctly deduplicated by the UNIQUE(symbol, timeframe, open_time) constraint.
func (s *Store) InsertBars(ctx context.Context, symbol, timeframe string, bars []model.OHLCV) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO ohlcv_bars (symbol, timeframe, open_time, open, high, low, close, volume)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, b := range bars {
		// For daily bars, truncate to date (YYYY-MM-DDT00:00:00Z) so that
		// the same trading day from different sources (IBKR vs yfinance)
		// or timezone contexts always maps to one unique row.
		// For intraday bars, keep full UTC timestamp.
		var key string
		if timeframe == "1 day" || timeframe == "1d" {
			key = b.Timestamp.UTC().Truncate(24*time.Hour).Format(time.RFC3339)
		} else {
			key = b.Timestamp.UTC().Format(time.RFC3339)
		}
		_, err := stmt.ExecContext(ctx, symbol, timeframe, key,
			b.Open, b.High, b.Low, b.Close, b.Volume)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpsertOptionChain saves an option chain snapshot so freshness tracking can
// detect when options data was last fetched.  Only OI is stored (no Greeks).
func (s *Store) UpsertOptionChain(ctx context.Context, chain *model.OptionChain) error {
	if chain == nil || len(chain.Expirations) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO option_quotes (underlying, expiration, strike, option_type, open_interest, implied_volatility, snapshot_time)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(underlying, expiration, strike, option_type, snapshot_time) DO UPDATE SET
			open_interest=excluded.open_interest, implied_volatility=excluded.implied_volatility`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, exp := range chain.Expirations {
		for _, c := range exp.Calls {
			if _, err := stmt.ExecContext(ctx, chain.Underlying, exp.Expiration, c.Strike, "C", c.OpenInterest, c.ImpliedVolatility, now); err != nil {
				return err
			}
		}
		for _, p := range exp.Puts {
			if _, err := stmt.ExecContext(ctx, chain.Underlying, exp.Expiration, p.Strike, "P", p.OpenInterest, p.ImpliedVolatility, now); err != nil {
				return err
			}
		}
	}

	// Prune old snapshots: keep only the current snapshot_time per underlying.
	// Uses != instead of < to avoid lexicographic comparison issues with
	// mixed timezone offsets (e.g. "+08:00" vs "Z") in SQLite string comparison.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM option_quotes
		WHERE underlying = ? AND snapshot_time != ?`,
		chain.Underlying, now); err != nil {
		return err
	}

	return tx.Commit()
}

// GetBars retrieves historical bars for a symbol.
func (s *Store) GetBars(ctx context.Context, symbol, timeframe string, limit int) ([]model.OHLCV, error) {
	// Return the latest N bars in chronological order (ASC).
	// Subquery selects latest N by DESC, outer sorts ASC.
	rows, err := s.db.QueryContext(ctx, `
		SELECT open_time, open, high, low, close, volume FROM (
			SELECT open_time, open, high, low, close, volume
			FROM ohlcv_bars
			WHERE symbol = ? AND timeframe = ?
			ORDER BY open_time DESC
			LIMIT ?
		) ORDER BY open_time ASC`, symbol, timeframe, limit)
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
		symbol, time.Now().UTC().Format(time.RFC3339))
	return err
}

// RemoveFromWatchlist removes a symbol from the watchlist.
func (s *Store) RemoveFromWatchlist(ctx context.Context, symbol string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM watchlist WHERE symbol = ?`, symbol)
	return err
}

// SaveWatchlistSnapshot upserts a daily snapshot for a watchlist symbol.
func (s *Store) SaveWatchlistSnapshot(ctx context.Context, snap model.QuickSummary) error {
	now := time.Now().UTC()
	date := now.Format("2006-01-02")
	refreshedAt := now.Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO watchlist_snapshots
			(symbol, snapshot_date, price, trend, rsi, iv_rank, max_pain, pcr,
			 range_low_1s, range_high_1s, recommendation, opportunity_score, last_refreshed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(symbol, snapshot_date) DO UPDATE SET
			price=excluded.price, trend=excluded.trend, rsi=excluded.rsi,
			iv_rank=excluded.iv_rank, max_pain=excluded.max_pain, pcr=excluded.pcr,
			range_low_1s=excluded.range_low_1s, range_high_1s=excluded.range_high_1s,
			recommendation=excluded.recommendation, opportunity_score=excluded.opportunity_score,
			last_refreshed_at=excluded.last_refreshed_at`,
		snap.Symbol, date, snap.Price, snap.Trend, snap.RSI, snap.IVRank,
		snap.MaxPain, snap.PCR, snap.RangeLow1S, snap.RangeHigh1S,
		snap.Recommendation, snap.OpportunityScore, refreshedAt,
	)
	return err
}

// GetLatestSnapshots returns the most-recent watchlist_snapshot row per symbol,
// sorted by opportunity_score descending. Used by the web dashboard cache path.
// Includes all watchlist symbols, even if they have no snapshot yet (NULL values).
func (s *Store) GetLatestSnapshots(ctx context.Context) ([]model.QuickSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			w.symbol,
			COALESCE(ws.price, 0) as price,
			COALESCE(ws.trend, '') as trend,
			COALESCE(ws.rsi, 0) as rsi,
			COALESCE(ws.iv_rank, 0) as iv_rank,
			COALESCE(ws.max_pain, 0) as max_pain,
			COALESCE(ws.pcr, 0) as pcr,
			COALESCE(ws.range_low_1s, 0) as range_low_1s,
			COALESCE(ws.range_high_1s, 0) as range_high_1s,
			COALESCE(ws.recommendation, '') as recommendation,
			COALESCE(ws.opportunity_score, 0) as opportunity_score,
			COALESCE(ws.snapshot_date, '') as snapshot_date
		FROM watchlist w
		LEFT JOIN (
			SELECT symbol, price, trend, rsi, iv_rank, max_pain, pcr,
			       range_low_1s, range_high_1s, recommendation, opportunity_score,
			       snapshot_date
			FROM watchlist_snapshots
			WHERE (symbol, snapshot_date) IN (
				SELECT symbol, MAX(snapshot_date)
				FROM watchlist_snapshots
				GROUP BY symbol
			)
		) ws ON ws.symbol = w.symbol
		ORDER BY ws.opportunity_score DESC NULLS LAST, w.symbol ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snaps []model.QuickSummary
	for rows.Next() {
		var q model.QuickSummary
		if err := rows.Scan(
			&q.Symbol, &q.Price, &q.Trend, &q.RSI, &q.IVRank,
			&q.MaxPain, &q.PCR, &q.RangeLow1S, &q.RangeHigh1S,
			&q.Recommendation, &q.OpportunityScore, &q.SnapshotDate,
		); err != nil {
			return nil, err
		}
		snaps = append(snaps, q)
	}
	return snaps, rows.Err()
}

// DeleteWatchlistSnapshots removes all snapshot rows for a symbol.
func (s *Store) DeleteWatchlistSnapshots(ctx context.Context, symbol string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM watchlist_snapshots WHERE symbol = ?`, symbol)
	return err
}

// DeleteAnalysisCache removes the cached analysis JSON for a symbol.
func (s *Store) DeleteAnalysisCache(ctx context.Context, symbol string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM analysis_cache WHERE symbol = ?`, symbol)
	return err
}

// DeleteBackgroundJobs removes all background job records for a symbol.
func (s *Store) DeleteBackgroundJobs(ctx context.Context, symbol string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM background_jobs WHERE symbol = ?`, symbol)
	return err
}

// PruneStaleData removes expired data across all tables:
//   - background_jobs: keep only last 7 days
//   - ohlcv_bars: remove bars for symbols not in watchlist
//   - watchlist_snapshots: keep only last 90 days
//   - option_quotes: remove all rows with non-UTC snapshot_time (legacy cleanup)
//
// Safe to call periodically (e.g., daily from the scheduler).
func (s *Store) PruneStaleData(ctx context.Context) (int64, error) {
	var totalDeleted int64

	cutoff7d := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	cutoff90d := time.Now().UTC().Add(-90 * 24 * time.Hour).Format("2006-01-02")

	queries := []string{
		// Background jobs older than 7 days
		`DELETE FROM background_jobs WHERE created_at < '` + cutoff7d + `'`,
		// Watchlist snapshots older than 90 days
		`DELETE FROM watchlist_snapshots WHERE snapshot_date < '` + cutoff90d + `'`,
		// Bars for symbols not in watchlist
		`DELETE FROM ohlcv_bars WHERE symbol NOT IN (SELECT symbol FROM watchlist)`,
		// Quotes for symbols not in watchlist
		`DELETE FROM stock_quotes WHERE symbol NOT IN (SELECT symbol FROM watchlist)`,
	}

	for _, q := range queries {
		result, err := s.db.ExecContext(ctx, q)
		if err != nil {
			return totalDeleted, fmt.Errorf("prune: %w", err)
		}
		n, _ := result.RowsAffected()
		totalDeleted += n
	}

	return totalDeleted, nil
}

// SaveAnalysisCache persists a full analysis JSON payload for a symbol.
func (s *Store) SaveAnalysisCache(ctx context.Context, symbol string, payload []byte) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO analysis_cache (symbol, cached_at, payload_json)
		VALUES (?, ?, ?)
		ON CONFLICT(symbol) DO UPDATE SET
			cached_at=excluded.cached_at, payload_json=excluded.payload_json`,
		symbol, time.Now().UTC().Format(time.RFC3339), string(payload),
	)
	return err
}

// GetAnalysisCache retrieves a cached analysis payload for a symbol.
// Returns sql.ErrNoRows when no entry exists.
func (s *Store) GetAnalysisCache(ctx context.Context, symbol string) ([]byte, time.Time, error) {
	var cachedAt, payload string
	err := s.db.QueryRowContext(ctx,
		`SELECT cached_at, payload_json FROM analysis_cache WHERE symbol = ?`, symbol,
	).Scan(&cachedAt, &payload)
	if err != nil {
		return nil, time.Time{}, err
	}
	t, _ := time.Parse(time.RFC3339, cachedAt)
	return []byte(payload), t, nil
}

// GetWatchlist returns all watchlist symbols.
func (s *Store) GetWatchlist(ctx context.Context) ([]model.WatchlistItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol, added_at, notes, tags,
		       COALESCE(auto_refresh_enabled, 0) as auto_refresh_enabled,
		       COALESCE(refresh_interval_minutes, 15) as refresh_interval_minutes
		FROM watchlist
		ORDER BY added_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.WatchlistItem
	for rows.Next() {
		var item model.WatchlistItem
		var tags string
		var autoRefreshInt int
		if err := rows.Scan(&item.Symbol, &item.AddedAt, &item.Notes, &tags, &autoRefreshInt, &item.RefreshIntervalMinutes); err != nil {
			return nil, err
		}
		item.AutoRefreshEnabled = autoRefreshInt == 1
		// tags is JSON, but we store as simple string for now
		items = append(items, item)
	}
	return items, rows.Err()
}

// --- Data Freshness ---

// GetSymbolFreshness returns the last successful fetch timestamp for each
// data layer of a single symbol. Missing data layers have zero Time values.
func (s *Store) GetSymbolFreshness(ctx context.Context, symbol string) (model.SymbolFreshness, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE((SELECT updated_at  FROM stock_quotes  WHERE symbol    = ?1), '') AS quote_at,
			COALESCE((SELECT MAX(open_time) FROM ohlcv_bars WHERE symbol   = ?1 AND timeframe = '1 day'), '') AS ohlcv_at,
			COALESCE((SELECT MAX(snapshot_time) FROM option_quotes WHERE underlying = ?1), '') AS opt_at,
			COALESCE((SELECT cached_at   FROM analysis_cache WHERE symbol  = ?1), '') AS cache_at,
			COALESCE((SELECT MAX(last_refreshed_at) FROM watchlist_snapshots WHERE symbol = ?1), '') AS snap_date
	`, symbol)

	var quoteAt, ohlcvAt, optAt, cacheAt, snapDate string
	if err := row.Scan(&quoteAt, &ohlcvAt, &optAt, &cacheAt, &snapDate); err != nil {
		return model.SymbolFreshness{Symbol: symbol}, err
	}
	f := model.SymbolFreshness{Symbol: symbol}
	if quoteAt != ""  { f.QuoteAt, _   = time.Parse(time.RFC3339, quoteAt) }
	if ohlcvAt != ""  { f.OHLCVAt, _   = time.Parse(time.RFC3339, ohlcvAt) }
	if optAt != ""    { f.OptionsAt, _  = time.Parse(time.RFC3339, optAt) }
	if cacheAt != ""  { f.CacheAt, _   = time.Parse(time.RFC3339, cacheAt) }
	if snapDate != "" { f.SnapshotAt, _ = time.Parse(time.RFC3339, snapDate) }
	return f, nil
}

// GetAllSymbolFreshness returns freshness records for every symbol currently
// in the watchlist using a single JOIN query for efficiency.
func (s *Store) GetAllSymbolFreshness(ctx context.Context) ([]model.SymbolFreshness, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			w.symbol,
			COALESCE(sq.updated_at, '')           AS quote_at,
			COALESCE(ob.ohlcv_at, '')             AS ohlcv_at,
			COALESCE(oq.opt_at, '')               AS opt_at,
			COALESCE(ac.cached_at, '')            AS cache_at,
			COALESCE(ws.snap_date, '')            AS snap_date
		FROM watchlist w
		LEFT JOIN stock_quotes sq ON sq.symbol = w.symbol
		LEFT JOIN (
			SELECT symbol, MAX(open_time) AS ohlcv_at
			FROM ohlcv_bars WHERE timeframe = '1 day' GROUP BY symbol
		) ob ON ob.symbol = w.symbol
		LEFT JOIN (
			SELECT underlying, MAX(snapshot_time) AS opt_at
			FROM option_quotes GROUP BY underlying
		) oq ON oq.underlying = w.symbol
		LEFT JOIN analysis_cache ac ON ac.symbol = w.symbol
		LEFT JOIN (
			SELECT symbol, MAX(last_refreshed_at) AS snap_date
			FROM watchlist_snapshots GROUP BY symbol
		) ws ON ws.symbol = w.symbol
		ORDER BY w.symbol
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []model.SymbolFreshness
	for rows.Next() {
		var f model.SymbolFreshness
		var quoteAt, ohlcvAt, optAt, cacheAt, snapDate string
		if err := rows.Scan(&f.Symbol, &quoteAt, &ohlcvAt, &optAt, &cacheAt, &snapDate); err != nil {
			return nil, err
		}
		if quoteAt != ""  { f.QuoteAt, _   = time.Parse(time.RFC3339, quoteAt) }
		if ohlcvAt != ""  { f.OHLCVAt, _   = time.Parse(time.RFC3339, ohlcvAt) }
		if optAt != ""    { f.OptionsAt, _  = time.Parse(time.RFC3339, optAt) }
		if cacheAt != ""  { f.CacheAt, _   = time.Parse(time.RFC3339, cacheAt) }
		if snapDate != "" { f.SnapshotAt, _ = time.Parse(time.RFC3339, snapDate) }
		result = append(result, f)
	}
	return result, rows.Err()
}

// --- Background Refresh ---

// GetSymbolsNeedingRefresh returns symbols that need background refresh based on their
// auto_refresh_enabled flag and last refresh timestamp.
func (s *Store) GetSymbolsNeedingRefresh() ([]model.SymbolRefresh, error) {
	query := `
		SELECT
			symbol,
			refresh_interval_minutes,
			COALESCE(last_refreshed_at, '1970-01-01T00:00:00Z') as last_refresh
		FROM watchlist
		WHERE auto_refresh_enabled = 1
		  AND (
			  last_refreshed_at IS NULL
			  OR datetime(last_refreshed_at, '+' || refresh_interval_minutes || ' minutes') <= datetime('now')
		  )
		ORDER BY last_refreshed_at ASC NULLS FIRST
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []model.SymbolRefresh
	for rows.Next() {
		var sr model.SymbolRefresh
		var lastRefreshStr string

		if err := rows.Scan(&sr.Symbol, &sr.Interval, &lastRefreshStr); err != nil {
			continue
		}

		sr.LastRefresh, _ = time.Parse(time.RFC3339, lastRefreshStr)
		results = append(results, sr)
	}

	return results, nil
}

// UpdateLastRefreshTime updates the watchlist last refresh timestamp.
func (s *Store) UpdateLastRefreshTime(symbol string, t time.Time) error {
	_, err := s.db.Exec(`
		UPDATE watchlist
		SET last_refreshed_at = ?
		WHERE symbol = ?
	`, t.UTC().Format(time.RFC3339), symbol)
	return err
}

// UpdateWatchlistConfig updates auto-refresh settings for a symbol.
func (s *Store) UpdateWatchlistConfig(symbol string, enabled bool, intervalMinutes int) error {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	_, err := s.db.Exec(`
		UPDATE watchlist
		SET auto_refresh_enabled = ?,
			refresh_interval_minutes = ?
		WHERE symbol = ?
	`, enabledInt, intervalMinutes, symbol)
	return err
}

// CreateBackgroundJob inserts a new job record and returns its ID.
func (s *Store) CreateBackgroundJob(job *model.BackgroundJob) (int64, error) {
	result, err := s.db.Exec(`
		INSERT INTO background_jobs
		(symbol, job_type, status, started_at, error_message, retry_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		job.Symbol,
		job.JobType,
		job.Status,
		timeToString(job.StartedAt),
		job.ErrorMessage,
		job.RetryCount,
		job.CreatedAt.UTC().Format(time.RFC3339),
	)

	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// UpdateBackgroundJob updates job status after completion/failure.
func (s *Store) UpdateBackgroundJob(job *model.BackgroundJob) error {
	_, err := s.db.Exec(`
		UPDATE background_jobs
		SET status = ?,
			completed_at = ?,
			error_message = ?,
			retry_count = ?
		WHERE id = ?
	`,
		job.Status,
		timeToString(job.CompletedAt),
		job.ErrorMessage,
		job.RetryCount,
		job.ID,
	)
	return err
}

// GetBackgroundJob retrieves a single job by ID.
func (s *Store) GetBackgroundJob(id int64) (*model.BackgroundJob, error) {
	row := s.db.QueryRow(`
		SELECT id, symbol, job_type, status, started_at, completed_at,
			   error_message, retry_count, created_at
		FROM background_jobs
		WHERE id = ?
	`, id)

	var job model.BackgroundJob
	var startedStr, completedStr sql.NullString
	var createdStr string

	err := row.Scan(
		&job.ID,
		&job.Symbol,
		&job.JobType,
		&job.Status,
		&startedStr,
		&completedStr,
		&job.ErrorMessage,
		&job.RetryCount,
		&createdStr,
	)

	if err != nil {
		return nil, err
	}

	if startedStr.Valid {
		t, _ := time.Parse(time.RFC3339, startedStr.String)
		job.StartedAt = &t
	}

	if completedStr.Valid {
		t, _ := time.Parse(time.RFC3339, completedStr.String)
		job.CompletedAt = &t
	}

	job.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)

	return &job, nil
}

// GetBackgroundJobsForSymbol returns job history for a symbol.
func (s *Store) GetBackgroundJobsForSymbol(symbol string) ([]*model.BackgroundJob, error) {
	query := `
		SELECT id, symbol, job_type, status, started_at, completed_at,
			   error_message, retry_count, created_at
		FROM background_jobs
		WHERE symbol = ?
		ORDER BY created_at DESC
		LIMIT 20
	`

	rows, err := s.db.Query(query, symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*model.BackgroundJob
	for rows.Next() {
		var job model.BackgroundJob
		var startedStr, completedStr sql.NullString
		var createdStr string

		err := rows.Scan(
			&job.ID, &job.Symbol, &job.JobType, &job.Status,
			&startedStr, &completedStr, &job.ErrorMessage,
			&job.RetryCount, &createdStr,
		)

		if err != nil {
			continue
		}

		if startedStr.Valid {
			t, _ := time.Parse(time.RFC3339, startedStr.String)
			job.StartedAt = &t
		}

		if completedStr.Valid {
			t, _ := time.Parse(time.RFC3339, completedStr.String)
			job.CompletedAt = &t
		}

		job.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)

		jobs = append(jobs, &job)
	}

	return jobs, nil
}

// GetRecentFailures returns failed jobs from the last N hours.
func (s *Store) GetRecentFailures(hours int) ([]*model.BackgroundJob, error) {
	query := `
		SELECT id, symbol, job_type, status, started_at, completed_at,
			   error_message, retry_count, created_at
		FROM background_jobs
		WHERE status = 'failed'
		  AND retry_count >= 3
		  AND created_at > datetime('now', '-' || ? || ' hours')
		ORDER BY created_at DESC
		LIMIT 20
	`

	rows, err := s.db.Query(query, hours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*model.BackgroundJob
	for rows.Next() {
		var job model.BackgroundJob
		var startedStr, completedStr sql.NullString
		var createdStr string

		err := rows.Scan(
			&job.ID, &job.Symbol, &job.JobType, &job.Status,
			&startedStr, &completedStr, &job.ErrorMessage,
			&job.RetryCount, &createdStr,
		)

		if err != nil {
			continue
		}

		if startedStr.Valid {
			t, _ := time.Parse(time.RFC3339, startedStr.String)
			job.StartedAt = &t
		}

		if completedStr.Valid {
			t, _ := time.Parse(time.RFC3339, completedStr.String)
			job.CompletedAt = &t
		}

		job.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)

		jobs = append(jobs, &job)
	}

	return jobs, nil
}

// timeToString converts a nullable time pointer to RFC3339 string.
func timeToString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
