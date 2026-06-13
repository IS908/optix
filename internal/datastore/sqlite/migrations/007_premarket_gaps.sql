-- Market Intel M4: premarket gap-fill statistics (computed distribution, TTL-refreshed).
CREATE TABLE IF NOT EXISTS premarket_gap_stats (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol        TEXT NOT NULL,
    direction     TEXT NOT NULL,
    band          TEXT NOT NULL,
    fill_rate     REAL NOT NULL,
    sample_n      INTEGER NOT NULL,
    lookback_days INTEGER NOT NULL,
    as_of         TEXT NOT NULL,
    UNIQUE(symbol, direction, band)
);
CREATE INDEX IF NOT EXISTS idx_pm_gap_symbol ON premarket_gap_stats(symbol);
