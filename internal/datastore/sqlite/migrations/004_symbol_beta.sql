CREATE TABLE IF NOT EXISTS symbol_beta (
    symbol       TEXT PRIMARY KEY,
    beta         REAL NOT NULL,
    observations INTEGER NOT NULL,
    as_of        TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
