-- Market Intel M1: sparkline 5m bars for pulse assets (business asset IDs,
-- NOT stock tickers — separate key space from ohlcv_bars; 2-day rolling
-- retention via PruneStaleData).
CREATE TABLE IF NOT EXISTS market_pulse_bars (
    asset_id TEXT      NOT NULL,
    ts       TIMESTAMP NOT NULL,
    open     REAL,
    high     REAL,
    low      REAL,
    close    REAL      NOT NULL,
    volume   INTEGER,
    PRIMARY KEY (asset_id, ts)
);
