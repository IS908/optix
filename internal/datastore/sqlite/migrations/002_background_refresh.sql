-- Migration 002: Background Refresh System
-- Adds auto-refresh configuration to watchlist and background job tracking table

-- Note: SQLite doesn't support ALTER TABLE ADD COLUMN IF NOT EXISTS
-- The migrate002() function in sqlite.go handles idempotent column additions

-- Index for efficient scheduler queries
CREATE INDEX IF NOT EXISTS idx_watchlist_auto_refresh
ON watchlist(auto_refresh_enabled, refresh_interval_minutes)
WHERE auto_refresh_enabled = 1;

-- Background jobs table for task execution tracking
CREATE TABLE IF NOT EXISTS background_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol TEXT NOT NULL,
    job_type TEXT NOT NULL,        -- 'analyze' (future: 'dashboard')
    status TEXT NOT NULL,           -- 'pending', 'running', 'success', 'failed'
    started_at TEXT,                -- RFC3339 timestamp
    completed_at TEXT,              -- RFC3339 timestamp
    error_message TEXT,             -- NULL if success
    retry_count INTEGER DEFAULT 0,  -- Number of retry attempts
    created_at TEXT NOT NULL        -- RFC3339 timestamp
);

-- Indexes for common background_jobs queries
CREATE INDEX IF NOT EXISTS idx_background_jobs_symbol_created
ON background_jobs(symbol, created_at);

CREATE INDEX IF NOT EXISTS idx_background_jobs_status
ON background_jobs(status);
