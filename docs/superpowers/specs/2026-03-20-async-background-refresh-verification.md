# Async Background Refresh System - Verification Checklist

**Implementation Date**: 2026-03-20
**Feature Branch**: `feature/async-background-refresh`
**Design Spec**: `2026-03-20-async-background-refresh-design.md`

---

## Build Verification

- [x] **Unit tests pass**: All scheduler unit tests passing (5/5)
- [x] **Project compiles**: `go build ./cmd/optix-server` successful
- [ ] **Integration tests pass**: Requires IBKR TWS + Python server running
- [ ] **No linting errors**: Run `go vet ./...` and `golangci-lint run`

---

## Phase 1: Database Schema

### Verification Steps

1. **Check migration files exist**:
   ```bash
   ls -la internal/datastore/sqlite/migrations/
   # Should show: 001_initial_schema.sql, 002_background_refresh.sql
   ```

2. **Verify migration applies**:
   ```bash
   rm -f data/test.db
   ./bin/optix-server --db data/test.db &
   SERVER_PID=$!
   sleep 3
   kill $SERVER_PID
   ```

3. **Inspect schema**:
   ```bash
   sqlite3 data/test.db <<EOF
   .schema watchlist
   .schema background_jobs
   .indices watchlist
   .indices background_jobs
   EOF
   ```

   Expected columns in `watchlist`:
   - `auto_refresh_enabled INTEGER DEFAULT 0`
   - `refresh_interval_minutes INTEGER DEFAULT 15`
   - `last_refreshed_at TEXT`

   Expected `background_jobs` table with 9 columns:
   - id, symbol, job_type, status, started_at, completed_at, error_message, retry_count, created_at

4. **Test store methods** (manual query):
   ```bash
   sqlite3 data/test.db <<EOF
   INSERT INTO watchlist (symbol, auto_refresh_enabled, refresh_interval_minutes) VALUES ('TEST', 1, 5);
   SELECT * FROM watchlist WHERE symbol='TEST';
   EOF
   ```

**Status**: ☐ Pending manual verification

---

## Phase 2: Background Scheduler

### Verification Steps

1. **Unit tests passing**:
   ```bash
   go test ./internal/scheduler/... -v
   ```
   ✅ **PASSED** (5/5 tests)

2. **Batch generation logic**:
   - `TestBatchGeneration`: Verifies grouping by interval ✅
   - `TestGroupByInterval`: Verifies interval grouping ✅
   - `TestShouldDispatchBatch`: Verifies timing logic ✅

3. **Retry mechanism**:
   - `TestRetryDelayCalculation`: Verifies 1min → 5min → 15min delays ✅

4. **Worker pool**:
   - `TestMinFunction`: Helper function works ✅

**Status**: ✅ **VERIFIED** (unit tests passing)

---

## Phase 3: Frontend Polling

### Verification Steps - Dashboard

1. **Start server**:
   ```bash
   ./bin/optix-server
   ```

2. **Open browser DevTools → Network tab**

3. **Load** `http://localhost:8080/dashboard`

4. **Observe /api/freshness requests**:
   - Should see request every 30 seconds
   - Response should contain `{"watchlist": [...], "server_time": "..."}`

5. **Trigger background refresh** (simulate):
   ```bash
   sqlite3 data/optix.db "UPDATE watchlist SET last_refreshed_at='1970-01-01T00:00:00Z' WHERE symbol='AAPL';"
   ```

6. **Wait 30 seconds**:
   - Dashboard should auto-update when backend refresh completes
   - AAPL row should flash green (animation)
   - Check browser console for "Data changed for symbols: AAPL"

### Verification Steps - Analyze Page

1. **Load** `http://localhost:8080/analyze/AAPL`

2. **Open DevTools → Network**

3. **Observe /api/freshness polling**:
   - Requests every 30 seconds
   - Page reloads when `cache_at` changes for AAPL

**Status**: ☐ Pending manual verification

---

## Phase 4: Watchlist UI Configuration

### Verification Steps

1. **Open** `http://localhost:8080/watchlist`

2. **Click "Add Symbol"**

3. **Verify form fields**:
   - [ ] "启用自动后台刷新" checkbox visible
   - [ ] Refresh interval dropdown hidden by default
   - [ ] Dropdown shows when checkbox checked

4. **Add symbol with auto-refresh**:
   - Symbol: `TSLA`
   - Check "启用自动后台刷新"
   - Select "5分钟"
   - Submit

5. **Verify database**:
   ```bash
   sqlite3 data/optix.db "SELECT symbol, auto_refresh_enabled, refresh_interval_minutes FROM watchlist WHERE symbol='TSLA';"
   # Expected: TSLA|1|5
   ```

6. **Verify watchlist page shows badge**:
   - TSLA row should show: `⏱ 5分钟` badge (green)

7. **Add symbol WITHOUT auto-refresh**:
   - Symbol: `NVDA`
   - Leave checkbox unchecked
   - Submit

8. **Verify badge**:
   - NVDA row should show: `手动` badge (gray)

**Status**: ☐ Pending manual verification

---

## Phase 5: Server Integration

### Verification Steps

1. **Start server with verbose logging**:
   ```bash
   ./bin/optix-server
   ```

2. **Check startup logs**:
   ```
   [INFO] Background scheduler started workers=5 tick_interval=1m0s
   [INFO] Worker started worker_id=0
   [INFO] Worker started worker_id=1
   [INFO] Worker started worker_id=2
   [INFO] Worker started worker_id=3
   [INFO] Worker started worker_id=4
   Optix web UI  →  http://127.0.0.1:8080
   ```

3. **Verify scheduler runs**:
   - Wait 1 minute
   - Check logs for "Dispatching batch" or "Background task started"

4. **Monitor database**:
   ```bash
   watch -n 5 'sqlite3 data/optix.db "SELECT * FROM background_jobs ORDER BY created_at DESC LIMIT 5;"'
   ```

**Status**: ☐ Pending manual verification

---

## Phase 6: End-to-End Testing

### Prerequisites

- [x] Go server compiled and ready
- [ ] Python gRPC server running: `make py-server`
- [ ] IBKR TWS or IB Gateway running (port 7496)
- [ ] Watchlist contains at least one symbol

### Manual E2E Test Flow

1. **Setup**:
   ```bash
   # Start Python server
   cd python && make run

   # In another terminal, start IBKR TWS (live trading or paper trading)
   # Ensure it's running on port 7496

   # In another terminal, start Optix server
   ./bin/optix-server
   ```

2. **Add test symbol**:
   - Open `http://localhost:8080/watchlist`
   - Add `AAPL` with 5-minute auto-refresh enabled
   - Verify badge shows `⏱ 5分钟`

3. **Monitor scheduler logs** (wait up to 6 minutes):
   ```
   [DEBUG] Dispatching batch interval=5 batch_size=1 total_symbols=1
   [INFO] Background task started symbol=AAPL worker_id=0
   [INFO] Fetching quote for AAPL
   [INFO] Fetching OHLCV for AAPL
   [INFO] Fetching options for AAPL
   [INFO] Calling Python analysis for AAPL
   [INFO] Background task completed symbol=AAPL duration=45.2s
   ```

4. **Check database**:
   ```bash
   sqlite3 data/optix.db "SELECT * FROM background_jobs ORDER BY created_at DESC LIMIT 1;"
   # Expected: status=success, completed_at NOT NULL

   sqlite3 data/optix.db "SELECT symbol, cache_at FROM analysis_cache WHERE symbol='AAPL';"
   # Expected: Recent timestamp (within last 5 minutes)
   ```

5. **Check dashboard auto-update**:
   - Keep dashboard page open in browser
   - When next refresh happens (5 minutes later), observe:
     - AAPL row flashes green
     - Price/RSI/IV data updates
     - No page reload required

6. **Test failure scenario**:
   - Stop IBKR TWS
   - Wait for next scheduled refresh
   - Observe retry logs:
     ```
     [ERROR] Background task failed symbol=AAPL error="connection refused" retry_count=0
     [INFO] Scheduling retry in 1m0s
     ```
   - Restart TWS
   - Verify eventual success after retry

7. **Performance check** (20 stocks, 15-min interval):
   - Add 20 symbols to watchlist with 15-minute refresh
   - Monitor for 30 minutes
   - Verify batching: ~4 batches of 5 stocks spread over 15 minutes
   - Check CPU: `top -pid $(pgrep optix-server)` - should be <5%
   - Check memory: `ps aux | grep optix-server` - growth <50MB

### Integration Test (Automated)

```bash
# Requires Python server + IBKR TWS running
go test -tags=integration -v ./internal/scheduler/
```

**Expected**: TestSchedulerIntegration passes within 90 seconds

**Status**: ☐ Pending manual verification

---

## Regression Testing

### Existing Features Still Work

- [ ] Dashboard loads without ?refresh=true (cached data)
- [ ] Dashboard with ?refresh=true (live fetch) works
- [ ] Analyze page loads for cached symbols
- [ ] Analyze page with ?refresh=true works
- [ ] Watchlist add/remove functions
- [ ] Manual refresh buttons still work
- [ ] Navigation between pages works
- [ ] Error states display correctly

**Status**: ☐ Pending manual verification

---

## Performance Benchmarks

### Metrics to Record

1. **Scheduler overhead**:
   - CPU usage with 0 symbols: ___ %
   - CPU usage with 20 symbols (15-min refresh): ___ %
   - Memory baseline: ___ MB
   - Memory after 24 hours: ___ MB

2. **Task execution time**:
   - Average time per symbol (IBKR + Python): ___ seconds
   - P95 time: ___ seconds
   - P99 time: ___ seconds

3. **Frontend polling**:
   - /api/freshness response time: ___ ms
   - Network bandwidth per poll: ___ bytes
   - CPU impact of polling (browser): ___ %

**Status**: ☐ Pending benchmarking

---

## Security Review

- [ ] XSS prevention: All DOM updates use safe methods (no innerHTML)
- [ ] SQL injection: All queries use parameterized statements
- [ ] CSRF: Form submissions use POST (not GET)
- [ ] Rate limiting: Scheduler respects IBKR rate limits (12s between tasks)
- [ ] Error messages: No sensitive data leaked in error responses

**Status**: ☐ Pending security review

---

## Documentation Updates

- [x] Design spec written: `2026-03-20-async-background-refresh-design.md`
- [x] Implementation plan written: Plan file in `.claude/plans/`
- [ ] README.md updated with background refresh feature
- [ ] API documentation updated with /api/freshness endpoint
- [ ] User guide created for watchlist auto-refresh configuration

**Status**: ☐ Pending documentation

---

## Deployment Checklist

Before merging to `main`:

1. [ ] All phases verified manually
2. [ ] Integration tests passing
3. [ ] Performance benchmarks within targets
4. [ ] Security review completed
5. [ ] Documentation updated
6. [ ] Code review completed
7. [ ] Changelog entry added

**Branch**: `feature/async-background-refresh`
**Ready for merge**: ☐ No

---

## Known Issues / TODOs

- None identified yet

---

## Sign-off

- **Developer**: [Pending completion]
- **Reviewer**: [Pending code review]
- **Date**: [Pending]
