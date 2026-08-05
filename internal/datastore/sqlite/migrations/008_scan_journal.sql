-- Migration 008: Sell-put scan journal（扫描复盘闭环）
-- 追加式双表，照 006_intel_journal 模式。写路径只走 optix scan-journal CLI。

CREATE TABLE IF NOT EXISTS scan_candidates (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    candidate_id         TEXT UNIQUE NOT NULL,
    scan_date            TEXT NOT NULL,
    rank                 INTEGER NOT NULL,
    symbol               TEXT NOT NULL,
    right                TEXT NOT NULL DEFAULT 'P',
    expiry               TEXT NOT NULL,
    dte                  INTEGER NOT NULL,
    strike               REAL NOT NULL,
    spot                 REAL NOT NULL,
    bid                  REAL NOT NULL,
    ask                  REAL,
    mid                  REAL,
    iv                   REAL,
    delta                REAL,
    oi                   INTEGER,
    volume               INTEGER,
    cushion_pct          REAL NOT NULL,
    premium_yield_pct    REAL NOT NULL,
    annualized_yield_pct REAL NOT NULL,
    score                REAL NOT NULL,
    ibkr_bid             REAL,
    ibkr_ask             REAL,
    ibkr_option_iv       REAL,
    ibkr_option_delta    REAL,
    symbol_source        TEXT NOT NULL,
    created_at           TEXT NOT NULL,
    UNIQUE(scan_date, symbol, expiry, strike)
);
CREATE INDEX IF NOT EXISTS idx_scan_candidates_date   ON scan_candidates(scan_date);
CREATE INDEX IF NOT EXISTS idx_scan_candidates_expiry ON scan_candidates(expiry);

CREATE TABLE IF NOT EXISTS scan_reconciliations (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    candidate_id   TEXT UNIQUE NOT NULL,
    expiry_close   REAL NOT NULL,
    outcome        TEXT NOT NULL,
    realized_pnl   REAL NOT NULL,
    touched        INTEGER NOT NULL,
    max_breach_pct REAL NOT NULL,
    expiry_basis   TEXT NOT NULL,
    settled_at     TEXT NOT NULL
);
