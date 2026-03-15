-- Stock quotes (latest snapshot)
CREATE TABLE IF NOT EXISTS stock_quotes (
    symbol        TEXT PRIMARY KEY,
    last_price    REAL NOT NULL,
    bid           REAL,
    ask           REAL,
    volume        INTEGER,
    change_val    REAL,
    change_pct    REAL,
    high          REAL,
    low           REAL,
    open_price    REAL,
    close_price   REAL,
    high_52w      REAL,
    low_52w       REAL,
    avg_volume    REAL,
    updated_at    TEXT NOT NULL
);

-- Historical OHLCV bars
CREATE TABLE IF NOT EXISTS ohlcv_bars (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol        TEXT NOT NULL,
    timeframe     TEXT NOT NULL,
    open_time     TEXT NOT NULL,
    open          REAL NOT NULL,
    high          REAL NOT NULL,
    low           REAL NOT NULL,
    close         REAL NOT NULL,
    volume        INTEGER NOT NULL,
    UNIQUE(symbol, timeframe, open_time)
);
CREATE INDEX IF NOT EXISTS idx_ohlcv_symbol_time ON ohlcv_bars(symbol, timeframe, open_time);

-- Option chain snapshots
CREATE TABLE IF NOT EXISTS option_quotes (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    underlying         TEXT NOT NULL,
    expiration         TEXT NOT NULL,
    strike             REAL NOT NULL,
    option_type        TEXT NOT NULL,     -- 'C' or 'P'
    last_price         REAL,
    bid                REAL,
    ask                REAL,
    mid                REAL,
    volume             INTEGER,
    open_interest      INTEGER,
    implied_volatility REAL,
    delta              REAL,
    gamma              REAL,
    theta              REAL,
    vega               REAL,
    rho                REAL,
    snapshot_time      TEXT NOT NULL,
    UNIQUE(underlying, expiration, strike, option_type, snapshot_time)
);
CREATE INDEX IF NOT EXISTS idx_option_underlying_expiry ON option_quotes(underlying, expiration);

-- Watchlist
CREATE TABLE IF NOT EXISTS watchlist (
    symbol      TEXT PRIMARY KEY,
    added_at    TEXT NOT NULL,
    notes       TEXT DEFAULT '',
    tags        TEXT DEFAULT '[]'
);

-- Watchlist daily snapshots
CREATE TABLE IF NOT EXISTS watchlist_snapshots (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol            TEXT NOT NULL,
    snapshot_date     TEXT NOT NULL,
    price             REAL,
    trend             TEXT,
    rsi               REAL,
    iv_rank           REAL,
    max_pain          REAL,
    pcr               REAL,
    range_low_1s      REAL,
    range_high_1s     REAL,
    recommendation    TEXT,
    opportunity_score REAL,
    UNIQUE(symbol, snapshot_date)
);
