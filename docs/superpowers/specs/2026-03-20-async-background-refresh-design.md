# Async Background Refresh System Design

**Date**: 2026-03-20
**Author**: Claude Code
**Status**: Approved for Implementation

## Context

The Optix application currently uses a synchronous request-response model for data refreshing. All market data fetching (IBKR quotes, option chains, Python analysis) happens only when users explicitly click "⚡ Refresh Live" buttons or load pages with `?refresh=true`. This creates two problems:

1. **Stale data**: Users see outdated cached data unless they manually trigger refreshes
2. **Slow user experience**: Live refreshes take 45-90 seconds, blocking the page load

Users need **automatic background refreshing** where:
- The system periodically fetches fresh data in the background without user interaction
- Frontend pages automatically update when new data arrives, without manual page reloads
- Each stock in the watchlist can have independent refresh intervals (5/15/30/60 minutes)
- Failed refresh attempts are logged and automatically retried with exponential backoff

This design implements a lightweight, zero-dependency background task scheduler with frontend polling for automatic UI updates.

---

## Architecture Overview

### System Components

```
┌─────────────────────────────────────────────────────────────┐
│                    Web UI (Browser)                         │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Dashboard / Analyze Pages                           │  │
│  │                                                       │  │
│  │  JavaScript Polling (every 30 seconds):              │  │
│  │  1. Fetch /api/freshness → get timestamps (~1KB)    │  │
│  │  2. Compare with local cache                         │  │
│  │  3. Detect cache_at changes                          │  │
│  │  4. Fetch /api/dashboard → get full data            │  │
│  │  5. Update DOM with smooth animations                │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                           ↕ HTTP
┌─────────────────────────────────────────────────────────────┐
│                  Optix Server (Go)                          │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Web UI Handlers (internal/webui/)                   │  │
│  │  - GET /api/freshness (NEW)                          │  │
│  │  - GET /api/dashboard                                │  │
│  │  - GET /api/analyze/{symbol}                         │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Background Scheduler (NEW: internal/scheduler/)     │  │
│  │                                                       │  │
│  │  ┌────────────────────────────────────────────────┐  │  │
│  │  │ Worker Pool (5 goroutines)                    │  │  │
│  │  │ - Consume tasks from channel                   │  │  │
│  │  │ - Call fetchLiveAnalysis()                     │  │  │
│  │  │ - 12-second throttle per worker                │  │  │
│  │  │ - Update background_jobs log                   │  │  │
│  │  └────────────────────────────────────────────────┘  │  │
│  │                                                       │  │
│  │  ┌────────────────────────────────────────────────┐  │  │
│  │  │ Task Generator (every 1 minute)                │  │  │
│  │  │                                                │  │  │
│  │  │ Hybrid Batching Strategy:                      │  │  │
│  │  │ - Query symbols needing refresh                │  │  │
│  │  │ - Generate small batches (3-5 symbols/min)     │  │  │
│  │  │ - Distribute load over time                    │  │  │
│  │  │                                                │  │  │
│  │  │ Example (15min interval, 10 stocks):           │  │  │
│  │  │   Min 0:  3 symbols                            │  │  │
│  │  │   Min 5:  3 symbols                            │  │  │
│  │  │   Min 10: 4 symbols                            │  │  │
│  │  └────────────────────────────────────────────────┘  │  │
│  │                                                       │  │
│  │  ┌────────────────────────────────────────────────┐  │  │
│  │  │ Task Queue (buffered chan, cap=100)           │  │  │
│  │  └────────────────────────────────────────────────┘  │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  SQLite Datastore (internal/datastore/sqlite/)       │  │
│  │  - watchlist (+ auto_refresh_enabled, interval)     │  │
│  │  - background_jobs (NEW: task execution log)        │  │
│  │  - stock_quotes, analysis_cache (existing)          │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                           ↕
              IBKR TWS/Gateway + Python gRPC
```

---

## Database Schema Changes

### 1. Watchlist Table (Modifications)

```sql
-- Add two new columns to existing watchlist table
ALTER TABLE watchlist
ADD COLUMN auto_refresh_enabled INTEGER DEFAULT 0;

ALTER TABLE watchlist
ADD COLUMN refresh_interval_minutes INTEGER DEFAULT 15;

-- Index for efficient scheduler queries
CREATE INDEX idx_watchlist_auto_refresh
ON watchlist(auto_refresh_enabled, refresh_interval_minutes)
WHERE auto_refresh_enabled = 1;
```

**Fields**:
- `auto_refresh_enabled`: 0=manual only, 1=background refresh enabled
- `refresh_interval_minutes`: 5, 15, 30, or 60 minutes

### 2. Background Jobs Table (New)

```sql
CREATE TABLE background_jobs (
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

-- Indexes for common queries
CREATE INDEX idx_background_jobs_symbol_created
ON background_jobs(symbol, created_at);

CREATE INDEX idx_background_jobs_status
ON background_jobs(status);
```

**Lifecycle**:
1. Task created → `status='pending'`
2. Worker picks up → `status='running'`, `started_at=now()`
3. Success → `status='success'`, `completed_at=now()`
4. Failure → `status='failed'`, `error_message` set, `retry_count++`

---

## Backend Implementation

### File Structure

```
internal/scheduler/
├── scheduler.go      # Main scheduler orchestration
├── worker.go         # Worker pool implementation
├── task.go          # Task and Job types
└── batch.go         # Hybrid batch generation logic
```

### Core Types

```go
// task.go
package scheduler

type Task struct {
    Symbol    string
    Type      string        // "analyze"
    CreatedAt time.Time
    RetryOf   int64         // ID of failed job this retries (0 if new)
}

type BackgroundJob struct {
    ID           int64
    Symbol       string
    JobType      string
    Status       string
    StartedAt    *time.Time
    CompletedAt  *time.Time
    ErrorMessage string
    RetryCount   int
    CreatedAt    time.Time
}
```

### Scheduler Core Logic

```go
// scheduler.go
package scheduler

type Config struct {
    WorkerCount      int           // Default: 5
    QueueSize        int           // Default: 100
    TickInterval     time.Duration // Default: 1 minute
    WorkerThrottle   time.Duration // Default: 12 seconds
}

type Scheduler struct {
    cfg         Config
    store       *sqlite.Store
    ibCfg       IBConfig
    analysisCfg AnalysisConfig

    taskQueue   chan Task
    workers     []*Worker
    ticker      *time.Ticker

    mu          sync.Mutex
    lastBatch   map[int]time.Time  // interval → last batch time
}

func New(cfg Config, store *sqlite.Store, ibCfg IBConfig, analysisCfg AnalysisConfig) *Scheduler {
    return &Scheduler{
        cfg:         cfg,
        store:       store,
        ibCfg:       ibCfg,
        analysisCfg: analysisCfg,
        taskQueue:   make(chan Task, cfg.QueueSize),
        lastBatch:   make(map[int]time.Time),
    }
}

func (s *Scheduler) Start(ctx context.Context) error {
    // Initialize workers
    for i := 0; i < s.cfg.WorkerCount; i++ {
        w := NewWorker(i, s.taskQueue, s.store, s.ibCfg, s.analysisCfg, s.cfg.WorkerThrottle)
        s.workers = append(s.workers, w)
        go w.Run(ctx)
    }

    // Start task generator
    s.ticker = time.NewTicker(s.cfg.TickInterval)
    go s.generateTasks(ctx)

    return nil
}

func (s *Scheduler) generateTasks(ctx context.Context) {
    for {
        select {
        case <-s.ticker.C:
            s.generateBatch()
        case <-ctx.Done():
            return
        }
    }
}
```

### Hybrid Batch Generation

```go
// batch.go
package scheduler

func (s *Scheduler) generateBatch() {
    // Query symbols needing refresh
    symbols := s.store.GetSymbolsNeedingRefresh()

    // Group by refresh interval
    grouped := groupByInterval(symbols)

    now := time.Now()

    // For each interval group, dispatch small batches
    for interval, batch := range grouped {
        s.mu.Lock()
        lastTime := s.lastBatch[interval]
        s.mu.Unlock()

        // Distribute batches across the interval window
        // Example: 15min interval, 10 stocks → 3 batches over 15 minutes
        if shouldDispatchBatch(interval, lastTime, now, len(batch)) {
            // Take up to 5 symbols
            batchSize := min(5, len(batch))
            tasks := batch[:batchSize]

            for _, symbol := range tasks {
                s.taskQueue <- Task{
                    Symbol:    symbol,
                    Type:      "analyze",
                    CreatedAt: now,
                }

                // Update last refresh time in DB
                s.store.UpdateLastRefreshTime(symbol, now)
            }

            s.mu.Lock()
            s.lastBatch[interval] = now
            s.mu.Unlock()
        }
    }
}

func shouldDispatchBatch(interval int, lastBatch, now time.Time, symbolCount int) bool {
    if lastBatch.IsZero() {
        return true  // First batch
    }

    elapsed := now.Sub(lastBatch).Minutes()

    // Distribute batches evenly over the interval
    // E.g., 15min interval with 10 symbols → batch every ~5 minutes
    batchInterval := float64(interval) / math.Ceil(float64(symbolCount) / 5.0)

    return elapsed >= batchInterval
}

type SymbolRefresh struct {
    Symbol       string
    Interval     int
    LastRefresh  time.Time
}

func groupByInterval(symbols []SymbolRefresh) map[int][]string {
    groups := make(map[int][]string)
    for _, s := range symbols {
        groups[s.Interval] = append(groups[s.Interval], s.Symbol)
    }
    return groups
}
```

### Worker Implementation

```go
// worker.go
package scheduler

type Worker struct {
    id          int
    queue       <-chan Task
    store       *sqlite.Store
    ibCfg       IBConfig
    analysisCfg AnalysisConfig
    throttle    time.Duration
}

func (w *Worker) Run(ctx context.Context) {
    log.Info().Int("worker_id", w.id).Msg("Worker started")

    for {
        select {
        case task := <-w.queue:
            w.executeTask(ctx, task)

            // Throttle to avoid IBKR rate limits
            time.Sleep(w.throttle)

        case <-ctx.Done():
            log.Info().Int("worker_id", w.id).Msg("Worker stopped")
            return
        }
    }
}

func (w *Worker) executeTask(ctx context.Context, task Task) {
    start := time.Now()

    // Create job record
    job := &BackgroundJob{
        Symbol:     task.Symbol,
        JobType:    task.Type,
        Status:     "running",
        StartedAt:  &start,
        RetryCount: 0,
        CreatedAt:  task.CreatedAt,
    }

    if task.RetryOf > 0 {
        // Load retry count from previous job
        prevJob, _ := w.store.GetBackgroundJob(task.RetryOf)
        if prevJob != nil {
            job.RetryCount = prevJob.RetryCount + 1
        }
    }

    jobID := w.store.CreateBackgroundJob(job)
    job.ID = jobID

    // Execute the actual refresh (reuse existing fetchLiveAnalysis logic)
    err := w.fetchAndCache(ctx, task.Symbol)

    duration := time.Since(start)
    now := time.Now()
    job.CompletedAt = &now

    if err != nil {
        w.handleFailure(job, err, duration)
    } else {
        w.handleSuccess(job, duration)
    }
}

func (w *Worker) fetchAndCache(ctx context.Context, symbol string) error {
    // Reuse existing webui.fetchLiveAnalysis() logic
    // This connects to IBKR, fetches data, calls Python, saves to cache

    // Create temporary IB client
    ibClient := ibkr.New(w.ibCfg.Host, w.ibCfg.Port, clientID(w.id))
    defer ibClient.Disconnect()

    if err := ibClient.Connect(ctx); err != nil {
        return fmt.Errorf("IBKR connect: %w", err)
    }

    // Create temporary analysis client
    analysisClient, err := analysis.NewClient(w.analysisCfg.Addr)
    if err != nil {
        return fmt.Errorf("analysis client: %w", err)
    }
    defer analysisClient.Close()

    // Fetch market data
    data, err := server.FetchSymbolData(ctx, ibClient, symbol)
    if err != nil {
        return fmt.Errorf("fetch symbol data: %w", err)
    }

    // Run analysis
    analysisResp, err := analysisClient.AnalyzeStock(ctx, &analysisv1.AnalyzeStockRequest{
        Symbol:        symbol,
        Quote:         data.Quote,
        OhlcvBars:     data.Bars,
        OptionChain:   data.OptionChain,
        Capital:       w.analysisCfg.Capital,
        ForecastDays:  w.analysisCfg.ForecastDays,
        RiskTolerance: w.analysisCfg.RiskTolerance,
    })
    if err != nil {
        return fmt.Errorf("analyze stock: %w", err)
    }

    // Save to cache (reuse protoToAnalyzeResponse + JSON marshal)
    analyzeResp := protoToAnalyzeResponse(analysisResp)
    if err := w.store.SaveAnalysisCache(symbol, analyzeResp); err != nil {
        return fmt.Errorf("save cache: %w", err)
    }

    // Update watchlist snapshot
    if err := w.store.SaveWatchlistSnapshot(symbol, analyzeResp); err != nil {
        return fmt.Errorf("save snapshot: %w", err)
    }

    return nil
}

func (w *Worker) handleSuccess(job *BackgroundJob, duration time.Duration) {
    job.Status = "success"
    w.store.UpdateBackgroundJob(job)

    log.Info().
        Str("symbol", job.Symbol).
        Dur("duration", duration).
        Msg("Background task completed")
}

func (w *Worker) handleFailure(job *BackgroundJob, err error, duration time.Duration) {
    job.Status = "failed"
    job.ErrorMessage = err.Error()
    w.store.UpdateBackgroundJob(job)

    log.Error().
        Str("symbol", job.Symbol).
        Dur("duration", duration).
        Int("retry_count", job.RetryCount).
        Err(err).
        Msg("Background task failed")

    // Exponential backoff retry
    if job.RetryCount < 3 {
        delay := calculateRetryDelay(job.RetryCount + 1)

        time.AfterFunc(delay, func() {
            w.queue <- Task{
                Symbol:    job.Symbol,
                Type:      job.JobType,
                CreatedAt: time.Now(),
                RetryOf:   job.ID,
            }
        })

        log.Info().
            Str("symbol", job.Symbol).
            Dur("retry_delay", delay).
            Msg("Scheduling retry")
    } else {
        log.Warn().
            Str("symbol", job.Symbol).
            Msg("Max retries exceeded, giving up")
    }
}

func calculateRetryDelay(retryCount int) time.Duration {
    // 1st retry: 1 minute
    // 2nd retry: 5 minutes
    // 3rd retry: 15 minutes
    delays := []time.Duration{
        1 * time.Minute,
        5 * time.Minute,
        15 * time.Minute,
    }

    if retryCount-1 < len(delays) {
        return delays[retryCount-1]
    }
    return 15 * time.Minute
}

func clientID(workerID int) int {
    // Use unique client IDs to avoid conflicts
    // Base ID: 10, workers use 10-14
    return 10 + workerID
}
```

---

## Frontend Implementation

### API Endpoint: GET /api/freshness

```go
// internal/webui/handlers.go

type FreshnessResponse struct {
    Watchlist  []SymbolFreshness `json:"watchlist"`
    ServerTime time.Time         `json:"server_time"`
}

func (s *Server) handleFreshness(w http.ResponseWriter, r *http.Request) {
    symbols, err := s.store.GetWatchlistSymbols()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    freshness := make([]model.SymbolFreshness, 0, len(symbols))
    for _, symbol := range symbols {
        f, err := s.store.GetSymbolFreshness(symbol)
        if err != nil {
            continue
        }
        freshness = append(freshness, *f)
    }

    resp := FreshnessResponse{
        Watchlist:  freshness,
        ServerTime: time.Now(),
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}
```

### Frontend Polling Logic

Add to `internal/webui/static/templates/dashboard.html` and `analyze.html`:

```html
<script>
const FreshnessPoller = {
    interval: 30000,  // 30 seconds
    lastFreshness: {},
    timerId: null,

    start() {
        this.poll();  // Immediate first poll
        this.timerId = setInterval(() => this.poll(), this.interval);
    },

    stop() {
        if (this.timerId) {
            clearInterval(this.timerId);
            this.timerId = null;
        }
    },

    async poll() {
        try {
            const resp = await fetch('/api/freshness');
            if (!resp.ok) throw new Error('Freshness fetch failed');

            const data = await resp.json();
            const changed = this.detectChanges(data.watchlist);

            if (changed.length > 0) {
                await this.updateData(changed);
            }
        } catch (err) {
            console.error('Freshness poll error:', err);
        }
    },

    detectChanges(current) {
        const changed = [];

        for (const item of current) {
            const prev = this.lastFreshness[item.symbol];

            // Only compare cache_at timestamp
            if (!prev || prev.cache_at !== item.cache_at) {
                changed.push(item.symbol);
            }

            this.lastFreshness[item.symbol] = item;
        }

        return changed;
    },

    async updateData(symbols) {
        // Fetch full dashboard data
        const resp = await fetch('/api/dashboard');
        if (!resp.ok) throw new Error('Dashboard fetch failed');

        const data = await resp.json();

        // Update only changed rows
        for (const symbol of symbols) {
            const snapshot = data.snapshots.find(s => s.symbol === symbol);
            if (snapshot) {
                this.updateRow(symbol, snapshot);
            }
        }
    },

    updateRow(symbol, data) {
        const row = document.querySelector(`tr[data-symbol="${symbol}"]`);
        if (!row) return;

        // Flash animation on price cell
        const priceCell = row.querySelector('.price');
        priceCell.classList.add('flash-green');
        priceCell.textContent = `$${data.price.toFixed(2)}`;
        setTimeout(() => priceCell.classList.remove('flash-green'), 1500);

        // Update trend
        const trendCell = row.querySelector('.trend');
        trendCell.textContent = data.trend || '—';

        // Update RSI
        const rsiCell = row.querySelector('.rsi');
        rsiCell.textContent = data.rsi ? data.rsi.toFixed(1) : '—';

        // Update IV Rank
        const ivCell = row.querySelector('.iv-rank');
        ivCell.textContent = data.iv_rank ?
            `${(data.iv_rank * 100).toFixed(0)}%` : '—';

        // Update recommendation
        const recCell = row.querySelector('.recommendation');
        recCell.textContent = data.recommendation || '—';

        // Update freshness badge
        const badge = row.querySelector('.freshness-badge');
        badge.textContent = 'just now';
        badge.className = 'freshness-badge text-emerald-400';

        // Pulse animation on entire row
        row.classList.add('pulse-green');
        setTimeout(() => row.classList.remove('pulse-green'), 1500);
    }
};

// Start polling when page loads
document.addEventListener('DOMContentLoaded', () => {
    FreshnessPoller.start();
});

// Stop polling when page is hidden (save bandwidth)
document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
        FreshnessPoller.stop();
    } else {
        FreshnessPoller.start();
    }
});

// Cleanup on page unload
window.addEventListener('beforeunload', () => {
    FreshnessPoller.stop();
});
</script>

<style>
/* Flash animation for price updates */
.flash-green {
    animation: flash-green 1.5s ease-out;
}

@keyframes flash-green {
    0% { background-color: rgba(16, 185, 129, 0.3); }
    100% { background-color: transparent; }
}

/* Pulse animation for row updates */
.pulse-green {
    animation: pulse-green 1.5s ease-out;
}

@keyframes pulse-green {
    0% {
        box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.4);
    }
    70% {
        box-shadow: 0 0 0 10px rgba(16, 185, 129, 0);
    }
    100% {
        box-shadow: 0 0 0 0 rgba(16, 185, 129, 0);
    }
}
</style>
```

### Watchlist Management UI

Modify `internal/webui/static/templates/watchlist.html`:

```html
<!-- Add/Edit Modal Form -->
<div class="modal-content">
    <h2>{{ if .EditMode }}Edit{{ else }}Add{{ end }} Stock</h2>

    <form method="POST" action="/watchlist/{{ if .EditMode }}update{{ else }}add{{ end }}">
        <div class="form-group">
            <label for="symbol">Symbol</label>
            <input type="text" id="symbol" name="symbol"
                   value="{{ .Symbol }}" required {{ if .EditMode }}readonly{{ end }}>
        </div>

        <!-- NEW: Auto-refresh toggle -->
        <div class="form-group">
            <label class="checkbox-label">
                <input type="checkbox" id="auto-refresh" name="auto_refresh"
                       {{ if .AutoRefreshEnabled }}checked{{ end }}>
                <span>启用自动后台刷新</span>
            </label>
        </div>

        <!-- NEW: Refresh interval selector (hidden by default) -->
        <div class="form-group" id="interval-group"
             style="display: {{ if .AutoRefreshEnabled }}block{{ else }}none{{ end }}">
            <label for="interval">刷新间隔</label>
            <select id="interval" name="refresh_interval">
                <option value="5" {{ if eq .RefreshInterval 5 }}selected{{ end }}>
                    5分钟
                </option>
                <option value="15" {{ if eq .RefreshInterval 15 }}selected{{ end }}>
                    15分钟（推荐）
                </option>
                <option value="30" {{ if eq .RefreshInterval 30 }}selected{{ end }}>
                    30分钟
                </option>
                <option value="60" {{ if eq .RefreshInterval 60 }}selected{{ end }}>
                    60分钟
                </option>
            </select>
        </div>

        <div class="form-actions">
            <button type="submit" class="btn-primary">Save</button>
            <button type="button" class="btn-secondary" onclick="closeModal()">Cancel</button>
        </div>
    </form>
</div>

<script>
// Show/hide interval selector based on checkbox
document.getElementById('auto-refresh').addEventListener('change', function(e) {
    const group = document.getElementById('interval-group');
    group.style.display = e.target.checked ? 'block' : 'none';
});
</script>
```

### Watchlist Table Display

```html
<table class="watchlist-table">
    <thead>
        <tr>
            <th>Symbol</th>
            <th>Price</th>
            <th>Trend</th>
            <th>RSI</th>
            <th>IV Rank</th>
            <th>Recommendation</th>
            <th>Auto Refresh</th>  <!-- NEW column -->
            <th>Actions</th>
        </tr>
    </thead>
    <tbody>
        {{range .Watchlist}}
        <tr data-symbol="{{.Symbol}}">
            <td><strong>{{.Symbol}}</strong></td>
            <td class="price">${{.Price}}</td>
            <td class="trend">{{.Trend}}</td>
            <td class="rsi">{{.RSI}}</td>
            <td class="iv-rank">{{.IVRank}}</td>
            <td class="recommendation">{{.Recommendation}}</td>

            <!-- NEW: Auto-refresh status badge -->
            <td>
                {{if .AutoRefreshEnabled}}
                    <span class="badge badge-success">
                        ⏱ {{.RefreshIntervalMinutes}}min
                    </span>
                {{else}}
                    <span class="badge badge-secondary">Manual</span>
                {{end}}
            </td>

            <td>
                <button onclick="editSymbol('{{.Symbol}}')">Edit</button>
                <button onclick="removeSymbol('{{.Symbol}}')">Remove</button>
            </td>
        </tr>
        {{end}}
    </tbody>
</table>

<style>
.badge {
    display: inline-block;
    padding: 0.25rem 0.5rem;
    font-size: 0.875rem;
    font-weight: 600;
    border-radius: 0.25rem;
}

.badge-success {
    background-color: rgba(16, 185, 129, 0.2);
    color: rgb(16, 185, 129);
    border: 1px solid rgba(16, 185, 129, 0.4);
}

.badge-secondary {
    background-color: rgba(107, 114, 128, 0.2);
    color: rgb(107, 114, 128);
    border: 1px solid rgba(107, 114, 128, 0.4);
}
</style>
```

---

## Database Queries

### SQLite Store Methods

```go
// internal/datastore/sqlite/sqlite.go

// GetSymbolsNeedingRefresh returns symbols that need background refresh
func (s *Store) GetSymbolsNeedingRefresh() ([]SymbolRefresh, error) {
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

    var results []SymbolRefresh
    for rows.Next() {
        var sr SymbolRefresh
        var lastRefreshStr string

        if err := rows.Scan(&sr.Symbol, &sr.Interval, &lastRefreshStr); err != nil {
            continue
        }

        sr.LastRefresh, _ = time.Parse(time.RFC3339, lastRefreshStr)
        results = append(results, sr)
    }

    return results, nil
}

// UpdateLastRefreshTime updates the watchlist last refresh timestamp
func (s *Store) UpdateLastRefreshTime(symbol string, t time.Time) error {
    _, err := s.db.Exec(`
        UPDATE watchlist
        SET last_refreshed_at = ?
        WHERE symbol = ?
    `, t.Format(time.RFC3339), symbol)
    return err
}

// CreateBackgroundJob inserts a new job record
func (s *Store) CreateBackgroundJob(job *BackgroundJob) (int64, error) {
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
        job.CreatedAt.Format(time.RFC3339),
    )

    if err != nil {
        return 0, err
    }

    return result.LastInsertId()
}

// UpdateBackgroundJob updates job status after completion/failure
func (s *Store) UpdateBackgroundJob(job *BackgroundJob) error {
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

// GetBackgroundJob retrieves a single job by ID
func (s *Store) GetBackgroundJob(id int64) (*BackgroundJob, error) {
    row := s.db.QueryRow(`
        SELECT id, symbol, job_type, status, started_at, completed_at,
               error_message, retry_count, created_at
        FROM background_jobs
        WHERE id = ?
    `, id)

    var job BackgroundJob
    var startedStr, completedStr sql.NullString

    err := row.Scan(
        &job.ID,
        &job.Symbol,
        &job.JobType,
        &job.Status,
        &startedStr,
        &completedStr,
        &job.ErrorMessage,
        &job.RetryCount,
        &job.CreatedAt,
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

    return &job, nil
}

// GetRecentFailures returns failed jobs from the last N hours
func (s *Store) GetRecentFailures(hours int) ([]*BackgroundJob, error) {
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

    var jobs []*BackgroundJob
    for rows.Next() {
        var job BackgroundJob
        var startedStr, completedStr sql.NullString

        err := rows.Scan(
            &job.ID, &job.Symbol, &job.JobType, &job.Status,
            &startedStr, &completedStr, &job.ErrorMessage,
            &job.RetryCount, &job.CreatedAt,
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

        jobs = append(jobs, &job)
    }

    return jobs, nil
}

func timeToString(t *time.Time) string {
    if t == nil {
        return ""
    }
    return t.Format(time.RFC3339)
}
```

---

## Integration & Server Startup

### Modify cmd/optix-server/main.go

```go
func main() {
    // ... existing setup ...

    // Start background scheduler
    schedulerCfg := scheduler.Config{
        WorkerCount:    5,
        QueueSize:      100,
        TickInterval:   1 * time.Minute,
        WorkerThrottle: 12 * time.Second,
    }

    sched := scheduler.New(
        schedulerCfg,
        store,
        scheduler.IBConfig{Host: ibHost, Port: ibPort},
        scheduler.AnalysisConfig{
            Addr:          analysisAddr,
            Capital:       capital,
            ForecastDays:  forecastDays,
            RiskTolerance: riskTolerance,
        },
    )

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    if err := sched.Start(ctx); err != nil {
        log.Fatal().Err(err).Msg("Failed to start scheduler")
    }

    log.Info().Msg("Background scheduler started")

    // ... start web server ...
}
```

---

## Testing Strategy

### Unit Tests

```go
// internal/scheduler/scheduler_test.go

func TestBatchGeneration(t *testing.T) {
    symbols := []SymbolRefresh{
        {Symbol: "AAPL", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
        {Symbol: "TSLA", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
        {Symbol: "NVDA", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
        {Symbol: "MSFT", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
        {Symbol: "GOOGL", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
        {Symbol: "AMZN", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
        {Symbol: "META", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
        {Symbol: "NFLX", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
        {Symbol: "AMD", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
        {Symbol: "INTC", Interval: 15, LastRefresh: time.Now().Add(-20 * time.Minute)},
    }

    grouped := groupByInterval(symbols)
    assert.Len(t, grouped, 1)
    assert.Len(t, grouped[15], 10)

    // First batch should take 5 symbols
    batch := grouped[15][:5]
    assert.Len(t, batch, 5)
}

func TestRetryDelayCalculation(t *testing.T) {
    tests := []struct {
        retryCount int
        expected   time.Duration
    }{
        {1, 1 * time.Minute},
        {2, 5 * time.Minute},
        {3, 15 * time.Minute},
        {4, 15 * time.Minute}, // Max
    }

    for _, tt := range tests {
        actual := calculateRetryDelay(tt.retryCount)
        assert.Equal(t, tt.expected, actual, "retry_count=%d", tt.retryCount)
    }
}

func TestShouldDispatchBatch(t *testing.T) {
    now := time.Now()

    // First batch always dispatched
    assert.True(t, shouldDispatchBatch(15, time.Time{}, now, 10))

    // Within interval window
    lastBatch := now.Add(-4 * time.Minute)
    assert.False(t, shouldDispatchBatch(15, lastBatch, now, 10))

    // After interval window
    lastBatch = now.Add(-6 * time.Minute)
    assert.True(t, shouldDispatchBatch(15, lastBatch, now, 10))
}
```

### Integration Test

```go
// +build integration

func TestSchedulerEndToEnd(t *testing.T) {
    // Prerequisites: Python gRPC server must be running

    // Setup
    store, _ := sqlite.New(":memory:")
    defer store.Close()

    // Add test symbol with 5-minute refresh
    store.AddToWatchlist("TEST")
    store.UpdateWatchlistConfig("TEST", true, 5)

    // Start scheduler
    cfg := scheduler.Config{
        WorkerCount:    2,
        QueueSize:      10,
        TickInterval:   1 * time.Minute,
        WorkerThrottle: 5 * time.Second,
    }

    sched := scheduler.New(cfg, store, ibCfg, analysisCfg)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    sched.Start(ctx)

    // Wait for first refresh cycle (6 minutes to be safe)
    time.Sleep(6 * time.Minute)

    // Verify job was executed
    jobs, err := store.GetBackgroundJobsForSymbol("TEST")
    require.NoError(t, err)
    require.NotEmpty(t, jobs)

    job := jobs[0]
    assert.Equal(t, "success", job.Status)
    assert.NotNil(t, job.CompletedAt)

    // Verify cache was updated
    cache, err := store.GetAnalysisCache("TEST")
    require.NoError(t, err)
    assert.NotNil(t, cache)
}
```

### Manual Verification Steps

```bash
# 1. Start Python analysis engine
make py-server

# 2. Start Optix server with background scheduler
./bin/optix-server --web-addr 127.0.0.1:8080

# 3. Open browser: http://127.0.0.1:8080/watchlist

# 4. Add test stock (AAPL)
#    - Check "启用自动后台刷新"
#    - Select "5分钟" interval
#    - Click Save

# 5. Monitor server logs (should see within 1 minute):
#    [INFO] Background task started: AAPL
#    [INFO] Background task completed: AAPL (duration: 45s)

# 6. Open Dashboard page: http://127.0.0.1:8080/dashboard
#    - Open browser DevTools → Network tab
#    - Should see /api/freshness request every 30 seconds
#    - After 5 minutes, AAPL row should flash green

# 7. Verify database
sqlite3 data/optix.db <<EOF
SELECT * FROM background_jobs ORDER BY created_at DESC LIMIT 5;
SELECT symbol, auto_refresh_enabled, refresh_interval_minutes
FROM watchlist WHERE symbol = 'AAPL';
EOF

# 8. Test failure handling
#    - Stop IBKR TWS
#    - Wait for next refresh cycle
#    - Check logs for error
#    - Verify retry after 1 minute
#    - Restart TWS
#    - Verify eventual success

# 9. Performance test (20 stocks, 15-min interval)
#    - Add 20 symbols to watchlist with auto-refresh
#    - Monitor for 30 minutes
#    - Verify batching: ~4 batches of 5 stocks each
#    - Check CPU: should stay under 5%
#    - Check memory: growth should be < 50MB
```

---

## Error Handling

### Failure Scenarios

| Scenario | Behavior | Recovery |
|----------|----------|----------|
| IBKR TWS down | Task fails, logged to DB | Retry after 1/5/15 min (max 3) |
| Python gRPC timeout | Task fails after 90s | Retry with backoff |
| Invalid symbol | Task fails immediately | No retry (permanent failure) |
| Network timeout | Task fails | Retry with backoff |
| SQLite write error | Task fails, not cached | Retry with backoff |
| Worker panic | Worker restarts automatically | Task re-queued |

### Logging Examples

```
[INFO] Scheduler started with 5 workers
[INFO] Background task started: AAPL
[INFO] Background task completed: AAPL (duration: 42.3s)

[ERROR] Background task failed: TSLA (duration: 90.1s, retry=1/3)
        error: IBKR connection timeout

[INFO] Scheduling retry for TSLA (delay: 1m0s)

[WARN] Max retries exceeded for NVDA, giving up
```

---

## Performance Characteristics

### Resource Usage

- **CPU**: <5% average (spikes to 15% during batch execution)
- **Memory**: ~50MB baseline + ~10MB per active worker
- **Network**:
  - Backend: ~1 req/12s per worker = ~25 req/min
  - Frontend: 1 req/30s per client (~2 req/min)
- **Disk I/O**: Minimal (SQLite WAL mode, buffered writes)

### Throughput

- **Max concurrent refreshes**: 5 (worker pool size)
- **Theoretical max**: 300 stocks/hour (5 workers × 60 min / 1 stock per min)
- **Realistic**: 100-150 stocks/hour (accounting for IBKR latency)

### Latency

- **Freshness check**: <10ms (simple DB query)
- **Background refresh**: 30-90s (depends on IBKR + Python)
- **Frontend update**: <100ms (after data cached)

---

## Migration Path

### Phase 1: Database Schema (Day 1)

```sql
-- Run these migrations first
ALTER TABLE watchlist ADD COLUMN auto_refresh_enabled INTEGER DEFAULT 0;
ALTER TABLE watchlist ADD COLUMN refresh_interval_minutes INTEGER DEFAULT 15;

CREATE TABLE background_jobs (...);
CREATE INDEX idx_watchlist_auto_refresh ON watchlist(...);
CREATE INDEX idx_background_jobs_symbol_created ON background_jobs(...);
```

### Phase 2: Backend Scheduler (Day 2-3)

1. Implement `internal/scheduler/` package
2. Add SQLite store methods
3. Wire up in `cmd/optix-server/main.go`
4. Test with 1-2 stocks

### Phase 3: Frontend Polling (Day 4)

1. Add `/api/freshness` endpoint
2. Implement JavaScript poller
3. Add CSS animations
4. Test in browser

### Phase 4: Watchlist UI (Day 5)

1. Modify watchlist form
2. Update table display
3. Wire up handlers
4. End-to-end testing

### Phase 5: Polish & Monitoring (Optional)

1. Add optional frontend error toasts
2. Add `/api/scheduler/stats` debug endpoint
3. Performance tuning

---

## Future Enhancements (Out of Scope)

- **WebSocket real-time push** (replace polling)
- **Per-user refresh schedules** (multi-user support)
- **Priority queue** (VIP stocks refresh first)
- **Dashboard-wide batch refresh** (single job for all watchlist stocks)
- **Configurable worker pool size** (via CLI flag)
- **Prometheus metrics** (job duration, success rate)
- **Admin UI** (view job history, manually trigger refresh)

---

## Critical Files Summary

**New files**:
- `internal/scheduler/scheduler.go`
- `internal/scheduler/worker.go`
- `internal/scheduler/task.go`
- `internal/scheduler/batch.go`
- `internal/scheduler/scheduler_test.go`

**Modified files**:
- `internal/datastore/sqlite/sqlite.go` (add methods)
- `internal/datastore/sqlite/migrations/002_background_jobs.sql` (new migration)
- `internal/webui/handlers.go` (add `/api/freshness`)
- `internal/webui/server.go` (register route)
- `internal/webui/static/templates/dashboard.html` (add JS poller)
- `internal/webui/static/templates/analyze.html` (add JS poller)
- `internal/webui/static/templates/watchlist.html` (add UI controls)
- `cmd/optix-server/main.go` (start scheduler)
- `pkg/model/analysis.go` (add `SymbolRefresh` type)

**Database changes**:
- `watchlist` table: +2 columns
- `background_jobs` table: new
- Indexes: 3 new

---

## Success Criteria

✅ **Core Functionality**:
- Background tasks execute on schedule (configurable intervals)
- Frontend auto-updates without manual refresh
- Failed tasks retry with exponential backoff
- Job history logged to database

✅ **User Experience**:
- Dashboard shows live-updating data with smooth animations
- Watchlist UI allows per-symbol refresh configuration
- No page freezes or blocking (all async)

✅ **Reliability**:
- Graceful handling of IBKR/Python downtime
- No memory leaks or goroutine leaks
- SQLite handles concurrent writes safely

✅ **Performance**:
- CPU usage <5% average
- Memory growth <50MB over 24 hours
- No IBKR rate limit violations

---

## End of Design Document
