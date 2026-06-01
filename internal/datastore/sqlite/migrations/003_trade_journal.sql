CREATE TABLE IF NOT EXISTS trade_journal (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    exec_id     TEXT    NOT NULL UNIQUE,
    time        TEXT    NOT NULL,
    account     TEXT    NOT NULL,
    symbol      TEXT    NOT NULL,
    sec_type    TEXT    NOT NULL,
    expiration  TEXT    NOT NULL DEFAULT '',
    strike      REAL    NOT NULL DEFAULT 0,
    right       TEXT    NOT NULL DEFAULT '',
    side        TEXT    NOT NULL,
    quantity    REAL    NOT NULL,
    price       REAL    NOT NULL,
    avg_price   REAL    NOT NULL,
    currency    TEXT    NOT NULL DEFAULT 'USD',
    exchange    TEXT    NOT NULL DEFAULT '',
    order_id    INTEGER NOT NULL DEFAULT 0,
    perm_id     INTEGER NOT NULL DEFAULT 0,
    synced_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_tj_symbol  ON trade_journal(symbol);
CREATE INDEX IF NOT EXISTS idx_tj_time    ON trade_journal(time);
CREATE INDEX IF NOT EXISTS idx_tj_account ON trade_journal(account);

CREATE TABLE IF NOT EXISTS trade_journal_sync_state (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    last_sync_at    TEXT    NOT NULL,
    last_sync_count INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT    NOT NULL DEFAULT ''
);

INSERT OR IGNORE INTO trade_journal_sync_state (id, last_sync_at)
VALUES (1, '');
