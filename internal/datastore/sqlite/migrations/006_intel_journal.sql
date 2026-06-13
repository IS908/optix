-- Market Intel M3: judgment-journal closed loop (append-only audit log).
CREATE TABLE IF NOT EXISTS intel_narratives (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id      TEXT UNIQUE NOT NULL,
    trading_date  TEXT NOT NULL,
    checkpoint    TEXT NOT NULL,
    phase         TEXT NOT NULL,
    body          TEXT NOT NULL,
    created_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_intel_narratives_date ON intel_narratives(trading_date);

CREATE TABLE IF NOT EXISTS intel_judgments (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    judgment_id       TEXT UNIQUE NOT NULL,
    trading_date      TEXT NOT NULL,
    checkpoint        TEXT NOT NULL,
    asset_id          TEXT NOT NULL,
    asset_class       TEXT NOT NULL,
    direction         TEXT NOT NULL,
    threshold_pct     REAL NOT NULL DEFAULT 0,
    confidence        INTEGER NOT NULL,
    expiry_checkpoint TEXT NOT NULL,
    expiry_at         TEXT NOT NULL,
    registered_price  REAL NOT NULL,
    registered_basis  TEXT NOT NULL,
    rationale         TEXT,
    supersedes        TEXT,
    created_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_intel_judgments_date ON intel_judgments(trading_date);

CREATE TABLE IF NOT EXISTS intel_reconciliations (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    judgment_id   TEXT UNIQUE NOT NULL,
    expiry_price  REAL NOT NULL,
    expiry_basis  TEXT NOT NULL,
    outcome       TEXT NOT NULL,
    delta_pct     REAL NOT NULL,
    settled_at    TEXT NOT NULL
);
