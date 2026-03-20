# Full Integration Audit & Performance Optimization — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Verify data consistency across all layers, validate real-world behavior through browser testing with live IBKR data, fix any issues found, and optimize performance — iterating until clean.

**Architecture:** Three-phase audit: (1) Static code audit of 10 checklist items covering proto→Go→JSON→DB→JS data paths and Python metric calculations, (2) Browser E2E testing of 9 scenarios with real IBKR+Python services, (3) Performance baseline + optimization + iterative verification loop.

**Tech Stack:** Go (web server, gRPC client, SQLite), Python (gRPC analysis engine, Black-Scholes, technical indicators), IBKR TWS (market data), Chrome DevTools (browser testing), Lighthouse (performance audit).

**Spec:** `docs/superpowers/specs/2026-03-21-full-integration-audit-design.md`

---

## File Map

**Files to audit (read-only unless bugs found):**
- `internal/webui/proto_map.go` — Proto→Go response mapping (lines 13-153)
- `internal/webui/live.go` — Live data fetching & DB persistence (lines 29-254)
- `internal/webui/cache.go` — Cached data loading (lines 11-59)
- `internal/webui/handlers.go` — HTTP handlers, watchlist CRUD, maybeBackgroundRefresh (lines 30-271)
- `internal/webui/response_types.go` — All JSON response struct definitions (lines 22-154)
- `internal/webui/server.go` — Route registration, template setup (lines 37-128)
- `internal/datastore/sqlite/sqlite.go` — All DB queries (lines 223-499)
- `internal/scheduler/scheduler.go` — Task generation & batching (lines 14-161)
- `internal/scheduler/worker.go` — Worker execution pipeline (lines 19-297)
- `internal/scheduler/task.go` — Task struct (lines 5-11)
- `internal/scheduler/batch.go` — Batch grouping logic (lines 11-45)
- `internal/webui/static/templates/dashboard.html` — JS polling, IV display, freshness (lines 139-425)
- `internal/webui/static/templates/analyze.html` — JS polling, NoData state (lines 38-489)
- `internal/webui/static/templates/watchlist.html` — Add/remove forms (lines 153-381)
- `pkg/model/analysis.go` — QuickSummary, SymbolFreshness (lines 5-67)
- `python/src/optix_engine/grpc_server/analysis_servicer.py` — IV Rank, trend, HV20 (lines 20-689)
- `python/src/optix_engine/options/open_interest.py` — PCR, OI walls (lines 7-104)
- `python/src/optix_engine/options/max_pain.py` — Max pain algorithm (lines 7-53)
- `python/src/optix_engine/options/pricing.py` — Black-Scholes, Greeks
- `python/src/optix_engine/options/implied_vol.py` — IV solver
- `python/src/optix_engine/technical/indicators.py` — RSI, MACD, Bollinger (lines 17-61)
- `python/src/optix_engine/technical/support_resistance.py` — S/R detection (lines 94-149)
- `python/src/optix_engine/strategy/recommender.py` — Strategy scoring, PoP (lines 65-497)
- `proto/optix/analysis/v1/types.proto` — Proto message definitions

**Files likely to be modified (bug fixes):**
- `internal/webui/static/templates/dashboard.html` — IV Rank scaling fix, polling improvements
- `internal/webui/static/templates/analyze.html` — NoData poller fix
- `python/src/optix_engine/options/open_interest.py` — PCR infinity guard
- `internal/webui/live.go` — Freshness timestamp gaps
- `internal/datastore/sqlite/sqlite.go` — time.Parse error handling

---

## Phase 1: Static Code Audit

### Task 1: Proto → Go Mapping Audit

**Files:**
- Read: `internal/webui/proto_map.go:13-153`
- Read: `internal/webui/response_types.go:22-154`
- Read: `proto/optix/analysis/v1/types.proto`
- Read: `gen/go/optix/analysis/v1/types.pb.go`

- [ ] **Step 1: Read proto_map.go and compare every field mapping**

Read `internal/webui/proto_map.go` lines 13-132 (`protoToAnalyzeResponse()`).
For each section (Summary, Technical, Options, Outlook, Strategies), verify:
1. Every proto field in `types.proto` has a corresponding Go assignment
2. No type mismatches (e.g., int32 assigned to float64 without conversion)
3. No fields silently dropped (compare proto message field count vs Go assignment count)

Cross-reference with `response_types.go` to ensure JSON tags match expected API contract.

- [ ] **Step 2: Read snapToSymbolSummary and verify dashboard mapping**

Read `internal/webui/proto_map.go` lines 134-153 (`snapToSymbolSummary()`).
Verify all `model.QuickSummary` fields map to `SymbolSummary` JSON response fields.

- [ ] **Step 3: Document findings — fix any dropped fields or type mismatches**

If issues found, edit `proto_map.go` with fixes. If clean, document "Proto→Go mapping: PASS" in audit log.

- [ ] **Step 4: Commit if fixes applied**

```bash
git add internal/webui/proto_map.go
git commit -m "fix: correct proto→Go field mappings found during audit"
```

---

### Task 2: Live → DB Persistence Audit

**Files:**
- Read: `internal/webui/live.go:29-254`
- Read: `internal/datastore/sqlite/sqlite.go:223-244` (SaveWatchlistSnapshot)
- Read: `internal/datastore/sqlite/sqlite.go:311-321` (SaveAnalysisCache)

- [ ] **Step 1: Audit fetchLiveAnalysis() persistence path**

Read `live.go:29-101`. Verify:
1. Line 66-71: `SaveAnalysisCache` receives the full JSON-serialized `AnalyzeResponse`
2. Line 73-89: `SaveWatchlistSnapshot` receives ALL QuickSummary fields (price, trend, rsi, iv_rank, max_pain, pcr, range_low_1s, range_high_1s, recommendation, opportunity_score)
3. Line 91-99: Freshness timestamps — verify QuoteAt, OHLCVAt, OptionsAt are set from actual data timestamps (not just `time.Now()`), CacheAt and SnapshotAt set to now

- [ ] **Step 2: Audit fetchLiveDashboard() persistence path**

Read `live.go:103-254`. Verify:
1. Lines 206-220: Each symbol's snapshot saved with all fields from `BatchQuickAnalysis` proto response
2. Lines 224-246: Freshness array construction — check whether CacheAt is backfilled from DB or left zero (this is the `cache_at` vs `snapshot_at` semantic gap from spec item 5)
3. Confirm: Dashboard refresh does NOT update `analysis_cache`, so `CacheAt` stays stale after dashboard-only refresh

- [ ] **Step 3: Check SQLite INSERT statements match model fields**

Read `sqlite.go:223-244` (SaveWatchlistSnapshot INSERT). Count columns in SQL vs fields in `model.QuickSummary`. Verify 1:1 mapping and correct parameter order.

Read `sqlite.go:311-321` (SaveAnalysisCache INSERT). Verify payload_json and cached_at are saved correctly.

- [ ] **Step 4: Document cache_at semantic gap decision**

The dashboard poller compares `cache_at` to detect changes, but dashboard live refresh only updates `snapshot_at`. Decision needed:
- Option A: Dashboard poller should also compare `snapshot_at` (more responsive)
- Option B: Document as known behavior (dashboard refresh detected next time analysis runs)

If fixing, edit `dashboard.html` polling JS to also compare `snapshot_at`.

- [ ] **Step 5: Commit if fixes applied**

```bash
git add internal/webui/live.go internal/webui/static/templates/dashboard.html
git commit -m "fix: dashboard poller detects snapshot_at changes from live refresh"
```

---

### Task 3: DB → Cache Read Audit

**Files:**
- Read: `internal/webui/cache.go:11-59`
- Read: `internal/datastore/sqlite/sqlite.go:246-295` (GetLatestSnapshots)
- Read: `internal/datastore/sqlite/sqlite.go:323-335` (GetAnalysisCache)
- Read: `internal/datastore/sqlite/sqlite.go:366-441` (GetSymbolFreshness, GetAllSymbolFreshness)

- [ ] **Step 1: Audit GetLatestSnapshots SQL query**

Read `sqlite.go:246-295`. Verify:
1. LEFT JOIN ensures all watchlist symbols appear (even without snapshot data)
2. All watchlist_snapshots columns are selected and scanned into `model.QuickSummary` fields
3. NULL handling: `sql.NullFloat64` / `sql.NullString` used for LEFT JOIN nullable columns
4. ORDER BY opportunity_score DESC (highest opportunity first)

- [ ] **Step 2: Audit GetAnalysisCache JSON round-trip**

Read `sqlite.go:323-335`. Verify:
1. JSON payload stored as TEXT is retrieved and will be unmarshaled correctly
2. `cached_at` timestamp parsed with `time.Parse(time.RFC3339, ...)` — check if error is handled (not `_ =`)

- [ ] **Step 3: Audit freshness queries for time.Parse error swallowing**

Read `sqlite.go:366-441`. Search for patterns like:
```go
f.QuoteAt, _ = time.Parse(time.RFC3339, quoteAt)
```
Each `_ =` silently drops parse errors. If timestamps are malformed in DB, freshness will show "Never" without any error indication.

Decision: Either add logging for parse errors or check if timestamps are always written in RFC3339 format (they are, via `time.Now().Format(time.RFC3339)`). If all writes use RFC3339, the silent discard is safe. Document finding.

- [ ] **Step 4: Commit if fixes applied**

```bash
git add internal/datastore/sqlite/sqlite.go
git commit -m "fix: add logging for timestamp parse errors in freshness queries"
```

---

### Task 4: Frontend JS Field Reference Audit

**Files:**
- Read: `internal/webui/static/templates/dashboard.html:219-396`
- Read: `internal/webui/static/templates/analyze.html:450-489`
- Read: `internal/webui/response_types.go:22-154`

- [ ] **Step 1: Audit dashboard.html JS field references**

Read `dashboard.html:287-375` (`updateRow` function). For each cell update, verify the JS field name matches the Go JSON tag:
- `data.symbols[i].price` → `json:"price"` ✓
- `data.symbols[i].trend` → `json:"trend"` ✓
- `data.symbols[i].rsi` → `json:"rsi"` ✓
- `data.symbols[i].iv_rank` → `json:"iv_rank"` ✓
- `data.symbols[i].max_pain` → `json:"max_pain"` ✓
- `data.symbols[i].pcr` → `json:"pcr"` ✓
- `data.symbols[i].recommendation` → `json:"recommendation"` ✓
- `data.symbols[i].opportunity_score` → `json:"opportunity_score"` ✓

- [ ] **Step 2: CRITICAL — Audit IV Rank scaling**

Read `dashboard.html` around line 332. Check if JS does `data.iv_rank * 100`.
Then check `response_types.go` `SymbolSummary.IVRank` — is it 0-100 (from Python) or 0-1?

**Python** (`analysis_servicer.py:563`): `iv_rank = (current - min) / (max - min) * 100.0` → **already 0-100**

If JS does `* 100`, that's a **double-scaling bug** (65 → 6500%). Fix by removing the `* 100` in JS.

Also check Go template rendering (`dashboard.html` server-side): `{{printf "%.0f" .IVRank}}%` — if IVRank is already 0-100, this is correct.

- [ ] **Step 3: Audit analyze.html polling**

Read `analyze.html:450-489`. Verify:
1. Line 454: Symbol extracted from `{{.Summary.Symbol}}` — check what happens when `NoData=true` (Summary is zero-value, Symbol is "")
2. Line 452-483: Poller compares `cache_at` from `/api/freshness` — verify field name match

- [ ] **Step 4: Fix IV Rank scaling bug if confirmed**

Edit `dashboard.html` to remove `* 100` from IV Rank display in the JS polling update path.

```bash
git add internal/webui/static/templates/dashboard.html
git commit -m "fix: remove double-scaling of IV Rank in dashboard JS polling"
```

---

### Task 5: Freshness Data Linkage Audit

**Files:**
- Read: `internal/webui/static/templates/dashboard.html:139-217` (freshness panel)
- Read: `internal/webui/handlers.go:236-271` (handleFreshness)
- Read: `internal/datastore/sqlite/sqlite.go:393-441` (GetAllSymbolFreshness)

- [ ] **Step 1: Verify freshness panel color coding thresholds**

Read `dashboard.html:139-217`. Find the color coding logic:
- Green: data age < 6 hours
- Amber: 6-48 hours
- Red: > 48 hours
- Gray: "Never" (zero timestamp)

Verify thresholds are consistent in both server-side template and JS polling update.

- [ ] **Step 2: Verify /api/freshness includes ALL watchlist symbols**

Read `handlers.go:236-271`. Trace the query path:
1. `GetAllSymbolFreshness()` should use LEFT JOIN from watchlist to all data tables
2. New symbols with no data should appear with zero timestamps (not omitted)

Read `sqlite.go:393-441` to verify the LEFT JOIN structure.

- [ ] **Step 3: Verify cache_at vs snapshot_at in dashboard poller**

Read `dashboard.html:254-269` (`detectChanges`). Check which timestamp field(s) are compared:
- If only `cache_at`: dashboard-only refreshes (which update `snapshot_at` but not `cache_at`) won't be detected
- If both `cache_at` AND `snapshot_at`: both refresh paths detected

This is the semantic gap from Task 2 Step 4. Apply the decision made there.

- [ ] **Step 4: Commit if fixes applied**

```bash
git add internal/webui/static/templates/dashboard.html
git commit -m "fix: freshness polling compares both cache_at and snapshot_at"
```

---

### Task 6: Watchlist → Dashboard Linkage Audit

**Files:**
- Read: `internal/webui/handlers.go:30-85` (add/remove handlers)
- Read: `internal/datastore/sqlite/sqlite.go:246-295` (GetLatestSnapshots LEFT JOIN)

- [ ] **Step 1: Verify add-symbol creates watchlist entry that appears in dashboard**

Read `handlers.go:30-66` (handleWatchlistAdd). Trace:
1. Symbol parsed from form → `store.AddToWatchlist(symbol)`
2. After adding, dashboard's `GetLatestSnapshots()` uses LEFT JOIN → new symbol should appear with NULL data

- [ ] **Step 2: Verify remove-symbol cascades to all tables**

Read `handlers.go:68-85` (handleWatchlistRemove). Verify it calls:
1. `DeleteWatchlistSnapshots(symbol)` — removes from `watchlist_snapshots`
2. `DeleteAnalysisCache(symbol)` — removes from `analysis_cache`
3. `RemoveFromWatchlist(symbol)` — removes from `watchlist`

Check: Are `background_jobs` for this symbol also cleaned up? If not, stale jobs may reference a removed symbol.

- [ ] **Step 3: Fix missing cascade if found**

If background_jobs cleanup is missing:
1. Check if `DeleteBackgroundJobs(symbol)` method exists on the store. If not, implement it in `sqlite.go`:
   ```go
   func (s *Store) DeleteBackgroundJobs(ctx context.Context, symbol string) error {
       _, err := s.db.ExecContext(ctx, "DELETE FROM background_jobs WHERE symbol = ?", symbol)
       return err
   }
   ```
2. Add `store.DeleteBackgroundJobs(ctx, symbol)` to the remove handler in `handlers.go`.

- [ ] **Step 4: Commit if fixes applied**

```bash
git add internal/webui/handlers.go internal/datastore/sqlite/sqlite.go
git commit -m "fix: cascade watchlist removal to background_jobs table"
```

---

### Task 7: Analyze Background Refresh Linkage Audit

**Files:**
- Read: `internal/webui/server.go:56-74` (maybeBackgroundRefresh)
- Read: `internal/webui/static/templates/analyze.html:450-489` (poller)
- Read: `internal/webui/handlers.go:147-194` (handleAnalyze)

- [ ] **Step 1: Verify maybeBackgroundRefresh rate limiting**

Read `server.go:56-74`. Verify:
1. 3-minute cooldown per symbol using `lastRefreshMap`
2. Mutex protects concurrent access
3. Goroutine launched for background work
4. No error propagation to caller (fire-and-forget)

- [ ] **Step 2: Audit NoData poller edge case**

Read `analyze.html:450-489`. When `NoData=true`:
1. `{{.Summary.Symbol}}` renders as empty string ""
2. Poller initializes with `this.symbol = ""`
3. Guard `if (!this.symbol) return` prevents polling

**Result**: First-visit to a never-analyzed symbol triggers `maybeBackgroundRefresh` (server-side) but the page won't auto-reload when analysis completes. User sees stale "No Data" page.

**Fix**: Pass the symbol via a different template variable that's always set:

```html
<!-- In analyze.html, change symbol source -->
this.symbol = '{{.Symbol}}'  // .Symbol is always set from URL path
```

This requires `AnalyzeResponse` to always populate the `.Symbol` field (check `response_types.go`).

- [ ] **Step 3: Apply NoData poller fix**

Edit `analyze.html` line 454 to use `{{.Symbol}}` instead of `{{.Summary.Symbol}}`.
Verify `handlers.go:handleAnalyze` always sets `.Symbol` in the response (even when NoData=true).

- [ ] **Step 4: Commit**

```bash
git add internal/webui/static/templates/analyze.html internal/webui/handlers.go
git commit -m "fix: analyze page poller works even when NoData=true"
```

---

### Task 8: Scheduler Package Audit

**Files:**
- Read: `internal/scheduler/scheduler.go` (full file)
- Read: `internal/scheduler/worker.go` (full file)
- Read: `internal/scheduler/task.go` (full file)
- Read: `internal/scheduler/batch.go` (full file)

- [ ] **Step 1: Audit worker lifecycle and context cancellation**

Read `worker.go:57-74` (Run loop). Verify:
1. `ctx.Done()` channel is checked in the select statement
2. When context cancelled, any in-flight IBKR connection is closed
3. Worker goroutine exits cleanly (no leaked resources)

Read `worker.go:119-221` (fetchAndCache). Verify:
1. IBKR broker connection created with `defer broker.Disconnect()`
2. gRPC client connection created with `defer client.Close()`
3. Context passed through to all blocking calls

- [ ] **Step 2: Audit retry logic**

Read `worker.go:237-297` (handleFailure, calculateRetryDelay). Verify:
1. `retry_count` incremented in DB via `UpdateJobStatus`
2. Retry delays: 1min, 5min, 15min (exponential)
3. Max retries enforced (check if there's a max — if not, document)
4. Failed jobs don't block the queue

- [ ] **Step 3: Audit IBKR client ID allocation**

Read `worker.go` for client ID assignment. Verify:
- Web UI: client ID 4 (analyze), 5 (dashboard) — set in `live.go`
- Scheduler workers: should use IDs 10-14 (or similar non-overlapping range)
- If workers use fixed/overlapping IDs, IBKR will reject concurrent connections

Check `worker.go:119-221` for how `ibkr.New()` is called — look for client ID parameter.

**If conflicting IDs found**: Update worker client ID allocation to use `workerID + 10` (e.g., worker 0 → clientID 10, worker 1 → 11, etc.) to avoid conflicts with web UI IDs 4 and 5.

- [ ] **Step 4: Audit concurrent access safety**

When both `maybeBackgroundRefresh` (goroutine from web server) and scheduler worker refresh the same symbol:
1. `SaveWatchlistSnapshot` uses UPSERT (INSERT OR REPLACE) — safe, last write wins
2. `SaveAnalysisCache` uses UPSERT — safe, last write wins
3. SQLite WAL mode allows concurrent writes — but check if Go driver serializes writes

No race condition on data corruption, but possible duplicate work. Document as acceptable.

- [ ] **Step 5: Fix any issues found**

```bash
git add internal/scheduler/
git commit -m "fix: scheduler worker issues found during audit"
```

---

### Task 9: POST Endpoint Input Validation Audit

**Files:**
- Read: `internal/webui/handlers.go:30-85`

- [ ] **Step 1: Audit handleWatchlistAdd input validation**

Read `handlers.go:30-66`. Check:
1. Empty symbol input → should return error, not add empty string to DB
2. Invalid characters → should reject non-alphanumeric tickers
3. Duplicate symbol → `AddToWatchlist` should handle gracefully (INSERT OR IGNORE)
4. Whitespace handling → leading/trailing spaces stripped, symbols uppercased

- [ ] **Step 2: Audit handleWatchlistRemove cascading**

Read `handlers.go:68-85`. Verify all related data is cleaned:
1. `watchlist` table entry removed
2. `watchlist_snapshots` for symbol removed
3. `analysis_cache` for symbol removed
4. `background_jobs` for symbol removed (check if this exists)

- [ ] **Step 3: Fix validation gaps**

If empty/invalid symbols can be added, add validation:
```go
symbol = strings.TrimSpace(strings.ToUpper(symbol))
if symbol == "" || !regexp.MustCompile(`^[A-Z]{1,6}(\.[A-Z])?$`).MatchString(symbol) {
    http.Error(w, "Invalid symbol", http.StatusBadRequest)
    return
}
```

- [ ] **Step 4: Commit if fixes applied**

```bash
git add internal/webui/handlers.go
git commit -m "fix: add input validation for watchlist add/remove endpoints"
```

---

### Task 10A: Metric Audit — Technical Indicators & IV

**Files:**
- Read: `python/src/optix_engine/technical/indicators.py:17-61`
- Read: `python/src/optix_engine/grpc_server/analysis_servicer.py:20-33,535-565`

- [ ] **Step 1: Audit technical indicators (RSI, MACD, Bollinger)**

Read `indicators.py:17-61`. Verify:
1. **RSI**: Period=14, uses Wilder's EMA (`ewm(alpha=1/14)`), output 0-100, handles division by zero
2. **MACD**: Fast=12, Slow=26, Signal=9, histogram = macd_line - signal_line
3. **Bollinger**: Period=20, multiplier=2.0, middle=SMA(20), upper/lower=middle±2σ

- [ ] **Step 2: Audit IV Rank / HV20 / IV Correction**

Read `analysis_servicer.py:535-565`. Verify:
1. **HV20**: `std(log_returns, ddof=1) * sqrt(252)`, floor 5%, default 30%
2. **IV Rank**: `(current - min) / (max - min) * 100`, output 0-100, defaults (50, 50) when <5 points
3. **IV Percentile**: `count(hv < current) / total * 100`, distinct from IV Rank
4. **IV Correction** (line 33): `_IV_HV_RATIO = 0.75` applied ONLY for pricing (not IV Rank/Percentile)

Verify at lines 129-139 that the correction factor is applied correctly and not leaking into rank/percentile.

- [ ] **Step 3: Cross-layer IV Rank scaling verification**

Trace IV Rank value end-to-end:
1. Python: `iv_rank = ... * 100.0` → sends 0-100 via gRPC
2. Proto: `double iv_rank = 5` → received as float64 in Go
3. Go template: `{{printf "%.0f" .IVRank}}%` → displays "65%" ✓ (if value is 65)
4. JS polling: Check if `data.iv_rank * 100` exists → would display "6500%" ✗

**Note**: If this was already fixed in Task 4, verify the fix is correct and complete here.

**Fix**: Remove `* 100` from JS if present.

- [ ] **Step 4: Commit if fixes applied**

```bash
git add internal/webui/static/templates/dashboard.html
git commit -m "fix: correct IV Rank scaling in dashboard JS"
```

---

### Task 10B: Metric Audit — PCR, Max Pain, Trend, Support/Resistance

**Files:**
- Read: `python/src/optix_engine/options/open_interest.py:81-104`
- Read: `python/src/optix_engine/options/max_pain.py:7-53`
- Read: `python/src/optix_engine/grpc_server/analysis_servicer.py:568-638`
- Read: `python/src/optix_engine/technical/support_resistance.py:94-149`

- [ ] **Step 1: CRITICAL — Audit PCR infinity edge case**

Read `open_interest.py:81-104`. When `call_oi_total = 0`:
- Python returns `float('inf')` for `put_total / 0`
- This flows through gRPC as a proto `double` field
- Go receives `math.Inf(1)`
- `encoding/json.Marshal` will **fail** on `+Inf` (returns error: "json: unsupported value: +Inf")

**This is a critical bug.** The entire `/api/dashboard` or `/api/analyze` response will fail to serialize.

**Fix in Python** (`open_interest.py`):
```python
if call_oi_total == 0:
    return 99.99  # Cap PCR at 99.99 instead of infinity
```

Or **fix in Go** (`proto_map.go` or `live.go`):
```go
if math.IsInf(pcr, 0) || math.IsNaN(pcr) {
    pcr = 99.99
}
```

- [ ] **Step 2: Audit Max Pain algorithm**

Read `max_pain.py:7-53`. Verify:
1. Iterates through all strikes as test_price
2. For each: sum(max(test_price - K, 0) * call_oi) + sum(max(K - test_price, 0) * put_oi)
3. Returns strike with minimum total pain
4. Returns 0.0 if no strikes (edge case)

- [ ] **Step 3: Audit trend scoring**

Read `analysis_servicer.py:568-638`. Verify:
1. Weights sum to 1.0: MA(0.35) + MACD(0.25) + RSI(0.20) + Volume(0.20) = 1.00 ✓
2. Output clipped to [-1.0, +1.0]
3. Direction thresholds: >0.30 bullish, <-0.30 bearish, else neutral
4. Sign convention: positive = bullish (verify consistent in JS color coding)

- [ ] **Step 4: Audit Support/Resistance detection**

Read `support_resistance.py:94-149`. Verify 6 sources: MAs, pivots(window=5), Fibonacci(60-bar), Bollinger, OI walls(top 5), max pain. Strength scoring reasonable.

- [ ] **Step 5: Commit PCR fix**

```bash
git add python/src/optix_engine/options/open_interest.py
git commit -m "fix: cap PCR at 99.99 to prevent JSON infinity error"
```

---

### Task 10C: Metric Audit — Strategy Scoring, PoP, Black-Scholes

**Files:**
- Read: `python/src/optix_engine/strategy/recommender.py:65-497`
- Read: `python/src/optix_engine/options/pricing.py` (full)
- Read: `python/src/optix_engine/options/implied_vol.py` (full)
- Read: `python/src/optix_engine/grpc_server/analysis_servicer.py:238-243`

- [ ] **Step 1: Audit strategy scoring and PoP**

Read `recommender.py:472-497`. Verify:
1. Score weights sum to 1.0: PoP(0.30) + R/R(0.25) + theta(0.20) + IV(0.15) + safety(0.10) = 1.00 ✓
2. Normalization: `max(0, min(1, (val - min) / (max - min)))` — bounded [0, 1]
3. Final score: sum of weighted normalized components × 100 → output 0-100
4. **PoP via delta**: sell put = `(1 - abs(delta)) * 100` — verify delta sign convention

Read `recommender.py:91-97`. Verify IV environment thresholds: ≥50 high, 30-49 medium, <30 low.

- [ ] **Step 2: Audit Black-Scholes and implied vol solver**

Read `pricing.py` and `implied_vol.py`. Verify:
1. BSM formula: standard `N(d1)`, `N(d2)` with dividend yield adjustment
2. Greeks: delta, gamma, theta (per day / 365), vega (per 1% / 100), rho
3. IV solver: Newton-Raphson with Brent fallback, σ range [0.001, 5.0], tolerance 1e-6

- [ ] **Step 3: Audit Range Forecast (1σ/2σ)**

Read `analysis_servicer.py:238-243`. Verify:
1. `price_move_1s = iv_for_pricing × price × sqrt(T/365)`
2. `range_low_1s = max(price - move, 0.01)`, `range_high_1s = price + move`
3. 2σ = 2× the move
4. Verify opportunity_score output range (0-100) matches frontend progress bar

- [ ] **Step 4: Commit if fixes applied**

```bash
git add python/src/optix_engine/
git commit -m "fix: metric calculation issues found during audit"
```

---

## Phase 2: Browser E2E Testing

### Task 11: Start Services

**Prerequisites:** IBKR TWS running on port 7496.

- [ ] **Step 1: Start Python gRPC server**

```bash
cd /Users/kevin/go/src/github.com/IS908/optix
python/.venv/bin/python -m optix_engine.grpc_server.server --addr=localhost:50052
```

Expected: "Server started on localhost:50052" (keep terminal open)

- [ ] **Step 2: Build and start Go web server**

```bash
cd /Users/kevin/go/src/github.com/IS908/optix
make build
./bin/optix-server --web-addr 127.0.0.1:8080 --ib-host 127.0.0.1 --ib-port 7496
```

Expected: "Web server listening on 127.0.0.1:8080" (keep terminal open)

- [ ] **Step 3: Verify both services are healthy**

```bash
curl -s http://127.0.0.1:8080/api/freshness | head -c 200
```

Expected: JSON response with `{"watchlist":...,"server_time":"..."}` (may be empty watchlist)

---

### Task 12: Watchlist Management E2E (Scenario 2.1)

- [ ] **Step 1: Navigate to watchlist page**

Open browser to `http://127.0.0.1:8080/watchlist`.
Take screenshot of empty state.

- [ ] **Step 2: Add symbols AAPL, TSLA, NVDA**

Click "Add Symbols" button. Enter "AAPL TSLA NVDA" in the input.
Submit form. Verify:
1. Success toast message appears
2. All 3 symbols appear in the watchlist table
3. Auto-refresh defaults are set correctly

- [ ] **Step 3: Test duplicate addition**

Try adding "AAPL" again. Verify graceful handling (no error, no duplicate row).

- [ ] **Step 4: Verify via API**

```bash
curl -s http://127.0.0.1:8080/api/freshness | python3 -m json.tool
```

Expected: All 3 symbols in `watchlist` array with zero timestamps.

---

### Task 13: Dashboard Cached Mode E2E (Scenario 2.2)

- [ ] **Step 1: Navigate to dashboard**

Open `http://127.0.0.1:8080/dashboard`.
Verify:
1. All 3 watchlist symbols appear as rows
2. New symbols show "—" or "N/A" for data fields
3. Freshness panel shows "Never" for all timestamps

- [ ] **Step 2: Inspect /api/dashboard response**

Check Network tab. Verify `/api/dashboard` returns:
```json
{
  "generated_at": "...",
  "from_cache": true,
  "symbols": [
    {"symbol": "AAPL", "no_data": true, ...},
    ...
  ],
  "freshness": [...]
}
```

---

### Task 14: Dashboard Live Refresh E2E (Scenario 2.3)

- [ ] **Step 1: Click "Refresh Live" button**

Click the refresh button. Monitor:
1. Network tab: POST/GET to `/api/dashboard?refresh=true`
2. Server logs: IBKR connection, Python gRPC calls
3. Wait for completion (1-3 minutes for 3 symbols)
4. **If >3 minutes**: Check server logs for timeout errors or IBKR connection issues. Verify TWS is responsive. If stuck, Ctrl+C and retry.

- [ ] **Step 2: Verify populated data**

After refresh completes, verify each row has real data:
- Price: non-zero, reasonable stock price
- Trend: "bullish", "bearish", or "neutral"
- RSI: 0-100 range
- IV Rank: 0-100 range (NOT 0-1, NOT >100)
- Max Pain: non-zero strike price
- PCR: reasonable ratio (0.5-3.0 typical, NOT infinity)
- Range: low < price < high
- Score: 0-100

- [ ] **Step 3: Verify freshness panel updates**

All timestamp badges should show "just now" or within last few minutes.
Color should be green (< 6 hours old).

- [ ] **Step 4: Inspect full API response**

```bash
curl -s http://127.0.0.1:8080/api/dashboard | python3 -m json.tool > /tmp/dashboard_response.json
```

Verify JSON is valid, no `null` where numbers expected, no `NaN` or `Infinity` values.

---

### Task 15: Dashboard Polling Linkage E2E (Scenario 2.4)

- [ ] **Step 1: Monitor freshness polling**

Open Network tab, filter for `/api/freshness`.
Wait 60+ seconds. Verify:
1. Requests appear every ~30 seconds
2. Response size is small (<1KB)
3. HTTP 200 status

- [ ] **Step 2: Check for auto-update behavior**

If background scheduler is running and a refresh completes during observation:
1. Dashboard row should flash green
2. Data values should update without page reload
3. Freshness timestamps should update

---

### Task 16: Analyze Page E2E (Scenario 2.5)

- [ ] **Step 1: Navigate to analyze page**

Click AAPL row or navigate to `http://127.0.0.1:8080/analyze/AAPL`.
Check server logs for `maybeBackgroundRefresh` trigger.

- [ ] **Step 2: Verify all data sections render**

If cached data exists from dashboard refresh:
1. **Summary**: Price, Change, 52-week range, Volume
2. **Technical**: Trend badge, MA lines, RSI gauge, MACD, Bollinger bands, S/R levels
3. **Options**: IV Current, IV Rank, IV Percentile, IV Environment, Max Pain, PCR, OI Clusters
4. **Outlook**: Direction, Confidence, 1σ/2σ ranges, Forecast period, Risk events
5. **Strategies**: Strategy cards with score, legs, P/L, PoP, breakeven

- [ ] **Step 3: Click "Fetch & Analyze Live"**

Click the live analysis button. Wait for completion.
Verify `/api/analyze/AAPL` response in Network tab.
Verify freshness strip updates.

- [ ] **Step 4: Inspect full analyze API response**

```bash
curl -s "http://127.0.0.1:8080/api/analyze/AAPL" | python3 -m json.tool > /tmp/analyze_response.json
```

Verify all sections populated, no JSON errors, reasonable values.

---

### Task 17: Cross-Page Data Linkage E2E (Scenario 2.6)

- [ ] **Step 1: Verify analyze→dashboard sync**

After analyzing AAPL in Task 16, navigate back to `/dashboard`.
Verify AAPL row reflects the most recent analysis data (should match analyze page values).

- [ ] **Step 2: Remove a symbol and verify cascade**

Navigate to `/watchlist`. Remove NVDA.
Navigate to `/dashboard`. Verify NVDA row is gone.
Check `/api/freshness` — NVDA should not appear.

---

### Task 18: Error & Edge Cases E2E (Scenario 2.7)

- [ ] **Step 1: Check browser console for errors**

Open Chrome DevTools Console tab.
Navigate through all pages: Dashboard → Analyze/AAPL → Watchlist → Help.
Verify: Zero JavaScript errors.

- [ ] **Step 2: Check for 4xx/5xx responses**

In Network tab, filter by status code.
Verify: No 4xx or 5xx responses during normal flow.

- [ ] **Step 3: Test market-closed behavior**

If market is currently closed, verify:
1. Dashboard shows last available data with stale freshness timestamps
2. Live refresh may return partial data (quote but no fresh options chain)
3. No crash or error dialog

- [ ] **Step 4: Test concurrent refresh**

Click "Refresh Live" on dashboard, then immediately navigate to `/analyze/TSLA` and click "Fetch & Analyze Live".
Verify: No crash, no data corruption, both complete (possibly with timeout on one).

- [ ] **Step 5: Test Python server down resilience**

1. Stop the Python gRPC server (Ctrl+C in the Python terminal)
2. Navigate to `/dashboard` — should show cached data (from_cache=true)
3. Click "Refresh Live" — should fail gracefully with error message, not crash
4. Navigate to `/analyze/AAPL` — should show cached data if available
5. Restart Python server and verify everything recovers

---

### Task 19: Scheduler Auto-Refresh E2E (Scenario 2.8)

- [ ] **Step 1: Enable auto-refresh for a symbol**

Navigate to `/watchlist`. Edit AAPL to enable auto-refresh with 5-minute interval.
Verify the setting is saved (check DB or reload page).

- [ ] **Step 2: Monitor scheduler activity**

Watch Go server logs for scheduler worker messages:
```
[scheduler] generating batch...
[worker-0] processing AAPL...
[worker-0] AAPL complete
```

Wait for at least one scheduled refresh cycle (up to 5 minutes).

- [ ] **Step 3: Verify freshness updates without manual action**

Check `/api/freshness` — AAPL timestamps should be more recent than the last manual refresh.
Dashboard should show updated data after polling detects the change.

---

### Task 20: Help Page Smoke Test (Scenario 2.9)

- [ ] **Step 1: Navigate to /help**

Open `http://127.0.0.1:8080/help`. Verify:
1. Page renders without errors
2. No console errors
3. Content is readable and formatted correctly

---

## Phase 3: Performance Optimization

### Task 21: Performance Baseline Measurements

- [ ] **Step 1: Measure cached API response times**

Run 3 times each, record median:

```bash
# Dashboard cached
for i in 1 2 3; do curl -o /dev/null -s -w "%{time_total}\n" http://127.0.0.1:8080/api/dashboard; done

# Freshness
for i in 1 2 3; do curl -o /dev/null -s -w "%{time_total}\n" http://127.0.0.1:8080/api/freshness; done

# Analyze cached
for i in 1 2 3; do curl -o /dev/null -s -w "%{time_total}\n" http://127.0.0.1:8080/api/analyze/AAPL; done
```

Target: dashboard <100ms, freshness <50ms, analyze <200ms.

- [ ] **Step 2: Measure response sizes**

```bash
curl -s http://127.0.0.1:8080/api/dashboard | wc -c
curl -s http://127.0.0.1:8080/api/freshness | wc -c
curl -s http://127.0.0.1:8080/api/analyze/AAPL | wc -c
```

Target: freshness <1KB for 20 symbols.

- [ ] **Step 3: Measure page load times**

Use Chrome DevTools Performance tab:
1. Dashboard page: DOMContentLoaded + Load event timing
2. Analyze page: DOMContentLoaded + Load event timing

- [ ] **Step 4: Record live refresh wall time**

Time the "Refresh Live" button for 3 symbols end-to-end.

- [ ] **Step 5: Save baseline measurements**

Save all measurements to a temporary file for comparison after optimization:

```bash
cat > /tmp/optix_perf_baseline.txt << 'EOF'
=== Performance Baseline (pre-optimization) ===
Date: $(date)

API Response Times (median of 3):
  /api/dashboard (cached): XXXms
  /api/freshness: XXXms
  /api/analyze/AAPL (cached): XXXms

Response Sizes:
  /api/dashboard: XXX bytes
  /api/freshness: XXX bytes
  /api/analyze/AAPL: XXX bytes

Page Load (DOMContentLoaded):
  Dashboard: XXXms
  Analyze: XXXms

Live Refresh (3 symbols): XXX seconds
EOF
```

---

### Task 22: Frontend Performance Audit

- [ ] **Step 1: Run Lighthouse audit**

Use Chrome DevTools → Lighthouse tab.
Run on `http://127.0.0.1:8080/dashboard` with categories: Performance, Accessibility, Best Practices, SEO.
Record scores.

- [ ] **Step 2: Audit Tailwind CDN impact**

Check `base.html` for Tailwind CSS loading method:
- If CDN: measure network time for CDN fetch
- If CDN and >200ms: consider bundling locally
- Decision: Document finding, implement if high-impact

- [ ] **Step 3: Audit JS polling efficiency**

Read `dashboard.html` polling code. Check:
1. Does it use `document.visibilitychange` to pause when tab is hidden?
2. If not, add visibility check to prevent unnecessary polling when tab is backgrounded:

```javascript
document.addEventListener('visibilitychange', function() {
    if (document.hidden) {
        FreshnessPoller.stop();
    } else {
        FreshnessPoller.start();
    }
});
```

- [ ] **Step 4: Commit frontend optimizations**

```bash
git add internal/webui/static/templates/
git commit -m "perf: add visibility-based polling pause, improve frontend performance"
```

---

### Task 23: Backend Performance Audit

- [ ] **Step 1: Audit SQLite query efficiency**

Check for missing indexes on frequently queried columns:

```sql
-- Queries that need indexes:
-- GetLatestSnapshots: watchlist_snapshots(symbol, snapshot_date)
-- GetAllSymbolFreshness: stock_quotes(symbol), ohlcv_bars(symbol, timeframe), option_quotes(underlying), analysis_cache(symbol)
-- GetSymbolsNeedingRefresh: watchlist(auto_refresh_enabled, last_refreshed_at)
```

Read `internal/datastore/sqlite/migrations/001_initial.sql` and `internal/datastore/sqlite/migrations/002_background_refresh.sql` for existing indexes.
Add any missing indexes.

- [ ] **Step 2: Audit gRPC connection lifecycle**

Read `internal/analysis/client.go`. Check:
1. Is the gRPC connection created once and reused? (Good)
2. Or created per-request? (Bad — add connection pooling)

Read `live.go` for how the analysis client is used in web handlers.

- [ ] **Step 3: Audit template caching**

Read `server.go` template setup. Check:
1. Templates parsed with `template.Must(template.ParseGlob(...))` at startup? (Good — cached)
2. Or parsed per-request? (Bad — cache them)

- [ ] **Step 4: Check for N+1 query patterns**

Read `cache.go` and `handlers.go`. Check if any handler makes multiple DB queries in a loop when a single JOIN query would suffice.

- [ ] **Step 5: Implement high-impact optimizations**

Apply fixes for any issues found (missing indexes, per-request connections, etc.)

```bash
git add internal/
git commit -m "perf: add missing SQLite indexes, optimize connection lifecycle"
```

---

### Task 24: Post-Optimization Verification

- [ ] **Step 1: Re-measure performance baselines**

Re-run all measurements from Task 21. Compare with pre-optimization baseline.

```bash
# Dashboard cached
for i in 1 2 3; do curl -o /dev/null -s -w "%{time_total}\n" http://127.0.0.1:8080/api/dashboard; done

# Freshness
for i in 1 2 3; do curl -o /dev/null -s -w "%{time_total}\n" http://127.0.0.1:8080/api/freshness; done

# Analyze cached
for i in 1 2 3; do curl -o /dev/null -s -w "%{time_total}\n" http://127.0.0.1:8080/api/analyze/AAPL; done
```

- [ ] **Step 2: Static audit re-check on changed files**

Re-read any files modified in Tasks 22-23. Verify no data consistency regressions were introduced (e.g., changed SQL altering query results, modified field mappings, broken template rendering). If regressions found, fix before re-measuring.

- [ ] **Step 3: Re-run key E2E scenarios**

Quick smoke test through browser:
1. Dashboard loads with data ✓
2. Freshness polling works ✓
3. Analyze page renders ✓
4. No console errors ✓

- [ ] **Step 4: Run existing integration tests**

```bash
cd /Users/kevin/go/src/github.com/IS908/optix
go test ./...
```

Expected: All tests pass. If Python server tests needed:
```bash
python/.venv/bin/python -m pytest python/tests/ -v
```

- [ ] **Step 5: Evaluate iteration need**

Compare post-optimization measurements with targets:
- `/api/dashboard` cached < 100ms?
- `/api/freshness` < 50ms?
- `/api/analyze` cached < 200ms?
- No high-impact issues remaining?
- No E2E regressions from smoke test?

If ALL targets met → proceed to Task 25.
If targets not met → loop back to Task 23 for next round of optimization (max 3 iterations).

---

### Task 25: Final Summary & Documentation

- [ ] **Step 1: Write audit results report**

Create `docs/superpowers/specs/2026-03-21-integration-audit-results.md` with:
1. Phase 1 findings: bugs found and fixed, clean areas
2. Phase 2 results: all E2E scenario pass/fail status
3. Phase 3 results: before/after performance measurements
4. Remaining items: any medium/low priority issues for future work

- [ ] **Step 2: Commit final report**

```bash
git add docs/superpowers/specs/2026-03-21-integration-audit-results.md
git commit -m "docs: add integration audit results report"
```

- [ ] **Step 3: Run final test suite**

```bash
go test ./...
python/.venv/bin/python -m pytest python/tests/ -v
```

All green = audit complete.
