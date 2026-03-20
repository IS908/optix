# Optix Full Integration Audit & Performance Optimization

**Date**: 2026-03-21
**Status**: Approved
**Scope**: Data consistency audit, browser E2E testing, performance optimization

## Context

The Optix project recently completed an async background refresh system implementation. 5 critical bugs were fixed and 16 integration tests pass. This spec defines a comprehensive audit to verify data consistency across all layers, validate real-world behavior through browser testing with live IBKR data, and identify performance optimization opportunities.

**Environment**: IBKR TWS on default port 7496, Python gRPC on 50052, Go web server on 8080.

---

## Phase 1: Code-Level Static Audit

**Goal**: Confirm all data transformation paths have no remaining inconsistencies.

### Audit Checklist

1. **Proto → Go mapping** (`internal/webui/proto_map.go`)
   - Verify `protoToAnalyzeResponse()` maps ALL proto fields to Go response structs
   - Check for type mismatches (int32 vs float64, etc.)
   - Confirm no fields are silently dropped

2. **Live → DB persistence** (`internal/webui/live.go`)
   - `fetchLiveDashboard()`: Verify all `SymbolSummary` fields are saved to `watchlist_snapshots`
   - `fetchLiveAnalysis()`: Verify full `AnalyzeResponse` JSON is saved to `analysis_cache`
   - Check freshness timestamps are set correctly (quote_at, ohlcv_at, options_at, cache_at, snapshot_at)

3. **DB → Cache read** (`internal/webui/cache.go`)
   - `fetchCachedDashboard()`: Verify all `watchlist_snapshots` columns are read into `SymbolSummary`
   - `fetchCachedAnalysis()`: Verify JSON unmarshaling preserves all fields
   - Check for zero-value handling (0.0 vs nil/missing)
   - Check for silent `time.Parse` error swallowing in `sqlite.go` (freshness timestamps parsed with `_ = time.Parse(...)`)

4. **Frontend JS field references**
   - Dashboard polling: Verify `data.symbols[].field_name` matches `DashboardResponse.Symbols` JSON tags
   - Analyze polling: Verify `cache_at` comparison logic works correctly
   - Freshness panel: Verify all timestamp field names match `FreshnessResponse.Watchlist` JSON tags
   - **IV Rank scaling**: Verify the value scale is consistent across proto (0-1 vs 0-100), Go struct, JSON response, and JS display (e.g., `data.iv_rank * 100` in JS vs `{{printf "%.0f" .IVRank}}%` in template)

5. **Freshness data linkage**
   - After background refresh completes, verify freshness timestamps update in DB
   - Verify `/api/freshness` includes ALL watchlist symbols (including newly added ones with zero timestamps)
   - Verify freshness panel color coding thresholds (green <6h, amber 6-48h, red >48h)
   - **`cache_at` vs `snapshot_at` semantic gap**: Dashboard live refresh updates `snapshot_at` (via `SaveWatchlistSnapshot`) but may NOT update `cache_at` (only set by `SaveAnalysisCache`). Verify whether the dashboard poller (which compares `cache_at`) correctly detects dashboard-only refreshes, or if it should also compare `snapshot_at`.

6. **Watchlist → Dashboard linkage**
   - Add symbol → verify appears in dashboard (even without data, via LEFT JOIN)
   - Remove symbol → verify removed from dashboard AND `watchlist_snapshots` AND `analysis_cache`

7. **Analyze background refresh linkage**
   - `maybeBackgroundRefresh()` triggers on page visit
   - 3-minute rate limit per symbol
   - After completion, frontend polling detects `cache_at` change and reloads page
   - **NoData edge case**: When `NoData=true`, `Summary.Symbol` is empty, so the analyze page poller reads an empty symbol and skips polling entirely. This means a first-visit background refresh won't auto-reload the page — user must manually refresh. Document or fix.

8. **Scheduler package audit** (`internal/scheduler/`)
   - Worker lifecycle: Verify workers handle context cancellation and IBKR connection teardown cleanly
   - Retry logic: Verify `retry_count` increments correctly and `GetRecentFailures` reports exhausted retries
   - Concurrent access: When both `maybeBackgroundRefresh` (web server) and scheduler trigger refresh for the same symbol simultaneously, verify no race condition on `SaveWatchlistSnapshot` or `SaveAnalysisCache`
   - IBKR client ID allocation: Verify scheduler workers use distinct client IDs (10-14) that don't conflict with web UI IDs (4 for analyze, 5 for dashboard)

9. **POST endpoint input validation**
   - `handleWatchlistAdd`: Verify symbol validation (empty, invalid tickers, duplicates)
   - `handleWatchlistRemove`: Verify cascading deletes (snapshots, analysis cache, background jobs)

### Output
- List of issues found with file:line references
- Fixes applied inline during audit (not batched)

---

## Phase 2: Browser Full-Chain Integration Test

**Prerequisites**: Python gRPC server + Go web server + IBKR TWS running.

### Test Flow

#### 2.1 Watchlist Management
- Navigate to `/watchlist`
- Add 2-3 symbols (e.g., AAPL, TSLA, NVDA)
- Verify success toast message appears
- Verify symbols appear in watchlist table with correct status
- Test duplicate addition (same symbol again) — verify graceful handling

#### 2.2 Dashboard — Cached Mode
- Navigate to `/dashboard`
- Verify all watchlist symbols appear (new ones show "no data" state)
- Check freshness panel — new symbols should show "Never" timestamps

#### 2.3 Dashboard — Live Refresh
- Click "Refresh Live" button
- Wait for completion (expect 1-3 minutes for 3 symbols; if >3 min, check server logs for timeout)
- Verify each row populates: Price, Trend, RSI, IV Rank, Max Pain, PCR, Range, Score
- Verify freshness panel updates (all timestamps → just now)
- Inspect `/api/dashboard` JSON response for field completeness

#### 2.4 Dashboard — Polling Linkage
- Wait 30+ seconds
- Verify `/api/freshness` polling requests appear in Network tab
- If background refresh completes, verify row auto-updates with flash-green animation

#### 2.5 Analyze Page — Full Analysis
- Click a symbol to navigate to `/analyze/AAPL`
- Verify `maybeBackgroundRefresh` triggers (check server logs)
- If cached data exists, verify all sections render: Summary, Technical, Options, Outlook, Strategies
- Click "Fetch & Analyze Live"
- Verify `/api/analyze/AAPL` response is complete
- Verify freshness strip updates

#### 2.6 Cross-Page Data Linkage
- After Analyze refresh, navigate back to Dashboard
- Verify the refreshed symbol's data is updated on Dashboard
- Delete a symbol from Watchlist
- Verify it disappears from Dashboard and Freshness

#### 2.7 Error & Edge Cases
- Check browser console for JS errors
- Verify no 4xx/5xx API responses
- Test with a symbol that has no options data
- Market hours: If market is closed, verify graceful handling of partial data (quote exists but stale, no live option chain)
- Concurrent refresh: Click "Refresh Live" while background refresh is running — verify no crash or data corruption
- Python server down: Stop Python gRPC server mid-test, verify dashboard falls back to cached data with error banner

#### 2.8 Scheduler Auto-Refresh E2E
- Enable auto-refresh for at least one symbol via Watchlist form (set to 5-minute interval)
- Wait for the scheduler to trigger a refresh cycle (check server logs for worker activity)
- Verify freshness timestamps update without any manual button click
- Verify dashboard polling detects the change

#### 2.9 Help Page Smoke Test
- Navigate to `/help`
- Verify page renders without errors

### Verification Method
- Chrome DevTools: Network tab for API responses, Console for errors
- Server logs for backend errors
- Screenshots at key states: empty dashboard, populated dashboard, analyze page, error states

---

## Phase 3: Performance Optimization

### 3.0 Baseline Measurements
Before optimizing, measure current performance:
- Record `/api/dashboard` cached response time (3 runs, take median)
- Record `/api/freshness` response size
- Record `/api/analyze/{symbol}` cached response time
- Record dashboard page load time (DOMContentLoaded + full load)
- Record live refresh wall time for 3 symbols

### 3.1 Frontend Performance
- **Lighthouse audit**: Accessibility, SEO, best practices scores
- **Tailwind CDN**: Evaluate if local build would improve load time
- **JS polling efficiency**: Verify 30s interval, page visibility API stops polling when hidden
- **DOM updates**: Check for unnecessary reflows during row updates

### 3.2 API Response Performance
- `/api/dashboard` response time: cached mode target <100ms
- `/api/freshness` response size: target <1KB for 20 symbols
- `/api/analyze/{symbol}` response time: cached mode target <200ms
- Check for N+1 query patterns in SQLite

### 3.3 Backend Performance
- **SQLite**: WAL mode, index usage on frequent queries
- **gRPC connection**: Reuse vs per-request creation
- **IBKR connections**: Worker connection lifecycle, client ID conflicts between web UI (4,5) and scheduler (10-14)
- **Concurrency**: Dashboard live refresh parallelism (currently 5)
- **Memory**: `analysis_cache` payload sizes for large watchlists
- **Template caching**: Verify `template.Must` usage

### 3.4 Optimization Priorities
- Rank findings by impact (high/medium/low)
- Implement high-impact optimizations
- Document medium/low items for future work

---

## Success Criteria

1. **Static audit**: Zero critical data inconsistencies across all transformation paths
2. **Browser E2E**: All 9 test scenarios pass with real IBKR data
3. **Performance**: No high-impact optimization issues remaining; baseline measurements recorded
4. **No regressions**: Existing 16 integration tests still pass
