# Changelog

All notable changes to optix are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) and
correspond to git tags (`vMAJOR.MINOR.PATCH`).

When a release is cut, the `[Unreleased]` section below is renamed to the new
version with its release date, and a fresh empty `[Unreleased]` block is added
above it.

## [Unreleased]

### Fixed

- **`optix server` could truncate its own graceful shutdown on SIGTERM/SIGINT,
  and then (across two further review passes) had no reliable bound on that
  shutdown at all** (#196, found during the #195 review; two further
  findings from adversarial re-review of each prior fix in turn). The root
  command's global signal handler (installed for every command via
  `PersistentPreRunE`) and `optix server`'s own `signal.NotifyContext`-driven
  shutdown (HTTP drain + broker pool close) both listened for the same
  signals — Go delivers a signal to every registered listener, so on SIGTERM
  both fired concurrently, and the root handler's cleanup+`os.Exit` could
  race and truncate the server's own shutdown. `optix server` now calls a
  new `cli.SuppressSignalExit()` at the top of its `RunE` so the root
  handler's next SIGTERM/SIGINT becomes a no-op and the server's
  `signal.NotifyContext` flow is the sole driver of shutdown. That fix
  removed the root handler's 5s watchdog+`os.Exit` from the picture, which
  had accidentally been bounding total shutdown time as a side effect of the
  very race it lost — leaving two gaps: (1) `signal.NotifyContext`'s internal
  relay goroutine only forwards the *first* signal, so a second
  SIGINT/SIGTERM sent during a hung shutdown went nowhere; (2)
  `brokerPool.close()` (`internal/webui/broker_pool.go`) disconnected all
  slots serially with no per-connection timeout, and ibapi's `Disconnect` can
  block indefinitely against a wedged IB Gateway — one stuck slot could hang
  shutdown forever. A first pass at (2) parallelized the `Disconnect` calls
  themselves (each raced against a 3s `disconnectTimeout`) but left the
  per-slot mutex acquisition sequential in the outer loop — and `reconnect()`
  can hold that same mutex for up to 15s while dialing (e.g. an async
  post-request reconnect still in flight at shutdown time), so a slot in
  that state blocked `close()` on the lock *before* any disconnect race even
  started, long past the watchdog deadline below. Fixed by moving the lock
  acquisition into each slot's own goroutine (so a held lock only stalls its
  own slot) and adding an overall `closeTimeout` bound around the whole
  `close()` call, so a stuck lock can no longer make shutdown block past it
  either. Restored the lost force-kill semantics with a new
  `armForceExitWatchdog` in `internal/cli/server.go`, armed once the
  shutdown context is canceled: it forces an immediate exit on either a
  second SIGINT/SIGTERM or a 10s absolute deadline, whichever comes first.
- 14 docs-drift items across `README.md`, `CLAUDE.md`,
  `skills/commands/optix/SKILL.md`, and `docs/user_manual.md` (#194):
  missing `option-quote` / `portfolio concentration|greeks|stress` CLI rows,
  missing `/api/intel/intraday/*` and ticker-zone (`/api/quotes`,
  `/api/quote/{symbol}`, `/api/freshness`) routes, missing
  `internal/intraday/`, `internal/intelshared/`, `internal/journal/`,
  `internal/portfolio/`, `internal/watchlist/`, and `internal/scanjournal/`
  in the architecture docs, a stale "`intel/` is zero-IBKR" claim (it wires
  in the IBKR-preferred `intraday`/`shockintel` planes), a stale "M3–M7
  slots" SKILL.md description predating the real intraday cards (#184), a
  factually wrong watchlist route (`POST /watchlist` and
  `POST /watchlist/{symbol}/remove`, not `/api/watchlist/add|remove`), and an
  outdated "no real-time option prices" limitations note superseded by
  `optix option-quote` (#187).

## [0.15.3] - 2026-08-08

Patch release for hybrid IBKR handshake retry budgeting (#192).

### Fixed

- **Handshake-retry budget could blow past the web UI broker pool's 15s
  reconnect timeout and defeat the yfinance fallback** (#192, audit
  follow-up to #189; reworked in a second review round to a hybrid retry
  policy — see below). A wedged gateway (TCP connects, `NextValidID` never
  arrives) had its handshake timeout marked retryable without limit, so a
  single `Connect()` could burn up to ~30.35s across three attempts. The
  pool's `reconnectTimeout` is 15s, so the second attempt reliably died on
  `ctx.Done()` mid-handshake — and `FallbackBroker.Connect`'s #41 guardrail,
  seeing `ctx.Err() != nil`, returned without ever trying yfinance.
  An initial fix made handshake timeouts non-retryable outright, but an
  adversarial review rejected that: #188/#189's wedge symptom *is* a
  handshake timeout on a zombie-held **primary** clientID, not error 326,
  and #189's whole fix was retrying onto a fresh fallback ID — never
  retrying timeouts regressed that escape hatch, and `optix positions`
  (fixed ClientID 4, no yfinance equivalent since it has no `AccountReader`)
  would hard-fail on the very first zombie-held wedge instead of recovering.
  The policy is now a **hybrid**: error 326 (clientID already in use) keeps
  retrying through the full candidates list, unchanged #189 behavior. A
  handshake timeout now retries **at most once** per `Connect()`, advancing
  to the next fallback clientID — because a fresh ID can succeed immediately
  against a zombie-held primary — but a *second* timeout aborts, since a
  truly wedged gateway times out on any ID. Per-attempt handshake windows
  are derived as `min(handshakeDefault, remaining ctx budget − 1s reserve)`
  rather than the earlier divide-by-candidate-count split, which
  double-punished the primary attempt (the pool path got 5s instead of the
  full 10s default before any retry had even happened); the reserved second
  is left deliberately unspent so the caller's `ctx` stays alive after our
  attempts give up, letting `FallbackBroker.Connect`'s #41 guardrail still
  proceed to its no-op yfinance `Connect` instead of returning `ctx.Err()`.
  If the remaining window would fall below 2s, no further attempt is made
  at all and the accumulated error is returned immediately.
- `finishConnectLocked` no longer drains `IbWrapper.disconnectCh` before
  starting the disconnect watcher. Each connect attempt gets a fresh wrapper
  (#189), so any signal already sitting there by that point is a genuine
  drop between `NextValidID` and finish, not stale state from a prior
  session — draining it left `connected=true` on a dead socket until the
  next 30s Ping cycle.
- Documented `ibkr.Client`'s single-connect contract: `wrapper`/`ibClient`
  are mutated under `c.mu` on reconnect but read lock-free by every request
  path, which is only safe because every caller constructs a new `Client`
  per `Connect()` today. Reusing an instance across more than one
  Connect/Disconnect cycle is unsupported.

## [0.15.2] - 2026-08-08

Patch release fixing option-quote quality (#193).

### Fixed

- Six audited correctness findings in the `optix option-quote` chain (#193,
  follow-up to #187):
  - **Contract-validation errors were silently swallowed and slow-failed.**
    `IbWrapper.Error` treated every IB error code below 2000 as informational
    noise, including 200 ("no security definition" — a bad strike/expiry/
    right), 162, 300, 321, and 354, which are genuine per-request failures.
    A validation request against a nonexistent strike burned the full 5s
    collection window and reported the misleading `no usable price data`
    instead of the real reason. These five codes are now explicitly
    whitelisted through to the request's error channel; every other
    sub-2000 code (farm-connectivity chatter etc.) is still dropped exactly
    as before.
  - **`option-quote` always waited out the full 5s window.** `ReqMktData`
    is called with `snapshot=false` (streaming), so `TickSnapshotEnd` —
    which only fires for snapshot requests — never arrived, and the pending
    quote had no other way to finish early. The wrapper now closes the
    request as soon as bid, ask, a mark/last price, IV, and delta have all
    arrived, falling back to the existing timeout when they don't.
  - **A put's open interest/volume could be silently overwritten by the
    call's.** `TickSize` accepted `OPTION_CALL_*`/`OPTION_PUT_*` ticks for
    either side regardless of which contract was requested, so whichever
    arrived last won. Ticks are now filtered to the requested side; the
    side-safe per-contract tick (86) is still always accepted.
  - **Mark price and implied volatility had no defined source priority.**
    `MODEL_OPTION`'s computed price and IV could arrive in any order
    relative to the genuine `MARK_PRICE` tick (generic tick 221) and the
    other IV-bearing computation ticks, so the reported values depended on
    tick arrival order. Mark now prefers a genuine `MARK_PRICE` tick over
    the model-computed fallback once one has arrived; IV now resolves with
    an explicit priority (`MODEL_OPTION` > `LAST_OPTION_COMPUTATION` >
    bid/ask computation midpoint).
  - **Position-marking errors collapsed into a generic message.**
    `GetOptionQuote` (used to mark option positions) reported a bare
    `no price data` even when IBKR had returned a specific error — the real
    detail only reached `Warnings`. It now surfaces that real IB error text
    when one is available.
  - **`option-quote` couldn't distinguish "gateway unreachable" from
    "contract invalid" by exit code**, and `FallbackBroker.GetOptionQuoteDetails`
    reported the account-data error message (`does not support account
    data`) for what is actually a market-data capability. `option-quote`
    now exits `5` (new `exitNoData`, documented alongside the existing
    codes) with the real IB error as the headline message when the gateway
    responded but the contract itself was invalid, keeping exit `2` for
    gateway/connection failures; it also gained a `--timeout` flag for
    scanner-style callers that need a hard subprocess budget (default
    unchanged: `ctx.Background()` plus the existing internal collection
    window). `FallbackBroker.GetOptionQuoteDetails` now returns the new
    `ErrMarketDataNotSupported` instead of `ErrAccountNotSupported`.

## [0.15.1] - 2026-08-07

Patch release repairing the IBKR-first intraday cards (#191).

### Fixed

- Six audited correctness findings in the IBKR-first intraday Movers/Sector
  Heatmap cards (#191), plus two additional bugs surfaced while verifying
  the fix live against IB Gateway:
  - **Bars pipeline returned empty on both real data paths (blocker).** The
    intraday adapter always called `GetHistoricalBars` with an empty
    `startDate`, which drove IBKR's duration selection to a blind "1 Y" (IBKR
    rejects a year of 5-min bars — pacing violation, silently empty) and
    yfinance's day-count to its 365-day default (yfinance's 5m endpoint caps
    at ~60 days — also silently empty, no warning). The adapter now derives
    a real `startDate` from the lookback window (NY calendar date), IBKR's
    duration mapping is distance-aware (`<2d→"1 D"`, `<8d→"1 W"`,
    `<32d→"1 M"`, else `"6 M"`; unchanged when no startDate is supplied), and
    yfinance's day-count now treats an empty `endDate` as "through now"
    (previously required both dates to compute anything) and floors at 1 day
    instead of clamping short spans up to 30; `fetcher.py`'s `days→period`
    mapping gained the missing `days<=1 → "1d"` bucket.
  - **Live-verification-only, not one of the six audited findings:** fixing
    the above surfaced that IB Gateway's actual intraday bar dates carry a
    trailing zone token (`"20260807 09:30:00 US/Eastern"`), which the
    existing date parser rejected outright — silently zeroing every
    IBKR-sourced bar's `Timestamp` and defeating the movers/heatmap
    same-session-day filter even with the bars fix in place. Also: rather
    than implementing the audit's prescribed IBKR bar-volume ×100
    lots-to-shares scaling, live verification showed the currently-vendored
    `github.com/scmhub/ibapi` client (Decimal-typed `Bar.Volume`) already
    reports bar volume in shares — applying ×100 anyway inflated real
    session volume by two orders of magnitude (e.g. a single 5-minute AAPL
    bar read back as ~127M shares). That scaling was **not** applied;
    IBKR bar volume is left as reported.
  - 8-second `LoadTimeout` truncation was silent for partial per-symbol
    failures — only a fully-empty result produced a warning. Per-symbol
    quote/bar fetch loops now check `ctx.Err()` and stop issuing new calls as
    soon as the deadline fires, and a partial shortfall now surfaces an
    explicit "N/M symbols unavailable" warning instead of rendering as a
    clean, quietly-truncated result.
  - The card header could claim `basis:"realtime"` next to a
    `warnings:["...unavailable"]` line with zero data, because the fallback
    basis label echoed the source's *nominal* basis rather than reflecting
    whether the load actually succeeded. On total load failure the header
    now reports the canonical-enum floor `"delayed"` instead. The invalid,
    unreachable `"degraded"` sentinel is gone, and the top-level basis
    aggregate no longer returns the non-enum value `"mixed"` when per-row
    bases disagree — it now picks the dominant real basis (per-row values
    are unaffected).
  - Session-bar volume of `0` was silently backfilled from the unrelated
    daily quote volume, mixing two different measures under one label; it's
    left at `0` now (existing warnings already surface the gap).
  - Every intraday endpoint call opened a fresh IBKR connect/disconnect —
    Movers and Sector Heatmap are separate polling endpoints, so one ~30s
    poll cycle drove two full broker sessions. A short-TTL (~25s, injectable
    clock) snapshot cache now lets both cards share one loaded snapshot per
    poll cycle.
  - A watchlist load error was silently swallowed
    (`watchlist, _ := h.Watchlist(ctx)`); the intraday handlers now append an
    explicit "watchlist unavailable: …" warning instead.

## [0.15.0] - 2026-08-05

Minor release adding the sell-put scan journal（可证伪扫描复盘闭环）.

### Added

- 可证伪扫描复盘闭环（scan journal）：migration 008 双追加表
  `scan_candidates`/`scan_reconciliations`；`internal/scanjournal` 包 +
  `optix scan-journal register|reconcile|stats` 命令族（写路径只走 CLI）。
  扫描器每日运行时自动入库当日 Top-10、结算所有已到期候选（hit=到期收盘>
  行权价；realized P&L=开仓 bid−max(0,行权价−到期收盘)；touched=存续期
  [scan_date,expiry] 闭区间任一日 low<行权价；7 日历日宽限期后 void），
  结算结果以「复盘」段附在飞书消息里；`stats --by score-band` 按 score
  三分位输出命中率/平均 P&L/触及率，直接回答「score 高的候选是否真的更
  安全」。journal 故障一律降级为消息内警示行，不阻断扫描发出；扫描熔断
  （>20% 行情失败）时跳过当日入库（既有候选的对账照常进行）。

## [0.14.30] - 2026-08-05

Patch release stopping IB Gateway zombie sessions from SIGTERM'd optix
processes (the "skill 用完后连不上" accumulation chain).

### Fixed

- Stop orphaned IB Gateway sessions / zombie clientIDs from accumulating when
  an optix CLI process is killed with SIGTERM (agent-harness timeouts, wrapper
  kills), which previously fed the #189 error-326 fallback-ID retry until
  Gateway could no longer accept new connections. `internal/cli/root.go` now
  registers each broker's `Disconnect` with the signal-cleanup registry (via
  `RegisterBrokerCleanup`, wired at all ~14 IBKR connect sites across
  `internal/cli/`) so SIGTERM/SIGINT actually disconnect the broker instead of
  skipping it along with the rest of the deferred cleanup; a 5s watchdog
  bounds a wedged/gateway-hung `Disconnect` so the process still exits
  promptly. `skills/commands/optix/optix.sh` now runs the `optix` binary as a
  tracked background job and forwards TERM/INT to it, instead of letting bash
  die on the signal and orphan the child mid-connection. The Lark Nasdaq-100
  scan (`scripts/lark_nasdaq100_sell_put_scan.py`) no longer uses
  `subprocess.run(timeout=)` (immediate, uncatchable SIGKILL on timeout) to
  invoke the CLI; a new `run_optix_subprocess` helper sends SIGTERM to the
  child's process group first, gives it a 5s grace period to disconnect
  cleanly, and only SIGKILLs if it's still alive after that.

## [0.14.29] - 2026-07-29

Minor release adding the Lark Nasdaq-100 sell-put income scan cron entry.

### Added

- `scripts/lark_nasdaq100_sell_put_scan.py`: Nasdaq-100 sell-put income scan
  as a Lark (飞书) cron entry. yfinance full-index screen (7–24 DTE puts,
  ~5–18% OTM, OI/bid/spread filters, scored against a 0.24-delta target),
  top candidates verified per-contract through `optix option-quote` (IBKR),
  dual-source Markdown table on stdout. Self-gates to 09:45–10:10 ET on NYSE
  trading days so two China-time cron entries (21:50/22:50) cover US DST with
  exactly one firing. Constituents from the Nasdaq official API →
  slickcharts → built-in fallback (Wikipedia's article no longer embeds the
  table); >20% fetch-failure circuit breaker (counting exceptions, not just
  empty quotes) invalidates the run instead of reporting a plausible empty
  result. Unknown-delta rows take the maximum score penalty rather than a
  free pass; ranking uses bid-based (seller-worst-case) premium yield with
  the annualized term capped so 7-DTE contracts don't structurally dominate.
  Adds `lxml` as a direct Python dependency.

## [0.14.28] - 2026-07-15

Patch release fixing repeated IBKR CLI handshake failures (#188).

### Fixed

- Detect IBKR client-ID-in-use error 326 during the connection handshake and
  retry non-master clients with bounded, process-scoped fallback IDs. Each
  retry receives a fresh wrapper and API client, while Client ID 0 remains a
  single-attempt master connection so cross-client execution visibility is
  preserved.
- Distinguish TCP connection failures from post-connect `NextValidID`
  handshake timeouts and include the endpoint and attempted Client IDs in
  terminal errors.

## [0.14.27] - 2026-07-09

Patch release adding IBKR single-option quote validation for #186.

### Added

- Add `optix option-quote SYMBOL --expiry YYYY-MM-DD --right C|P --strike N
  --format json` as an IBKR-only single-contract validation command. The JSON
  payload includes bid, ask, mid, mark, last, volume, open interest, implied
  volatility, Greeks when IBKR supplies them, timestamp, source, market data
  type, and structured warnings for unavailable fields.
- Add a detailed `broker.DetailedOptionQuoter` path and IBKR tick aggregation
  for option volume, open interest, IV/Greeks, mark price, and live/delayed
  market data type, while keeping the existing mark-only `OptionQuoter`
  contract for account position marking.

## [0.14.26] - 2026-06-26

Patch release adding real Market Intel intraday cards for #145.

### Added

- Add IBKR-first `/api/intel/intraday/movers` and
  `/api/intel/intraday/sector-heatmap` endpoints with explicit source/basis
  metadata, warning-aware degraded states, and watchlist plus curated liquid
  symbol coverage.
- Add Market Intel WebUI intraday movers and sector heatmap cards so the
  intraday view now renders real data surfaces instead of leaving those
  capabilities absent.

## [0.14.25] - 2026-06-16

Patch release hardening the IBKR reconnect logic against a flapping gateway
(#176, follow-up to v0.14.18).

### Fixed

- Broker pool now uses a per-slot dwell timer (`connectedSince`) and a
  `dwellWindow = 2 × healthCheckInterval` before clearing reconnect backoff.
  A single brief success no longer resets the failure-count growth that
  v0.14.18 introduced — the slot must hold a live IBKR connection for the
  dwell window before backoff relaxes. Without this, a flapping gateway
  (handshake-then-drop) could perpetually reset the backoff and drive
  reconnects every 30s through the racy `scmhub/ibapi` connect/teardown
  path.
- The genuine-unhealthy reconnect path in the background health checker is
  now also gated by `nextRetry` (the same gate the v0.14.18 switchback path
  already had). `acquire`'s on-demand reconnect remains ungated so
  user-facing requests still get a connection immediately. `p.reconnect`
  itself zeroes `connectedSince` whenever it replaces a broker, so acquire
  and release-async paths can't carry a stale dwell timer across broker
  replacements.

## [0.14.24] - 2026-06-16

Patch release narrowing the CLI vs HTTP `pulse` parity claim (#163).

### Fixed

- CLAUDE.md, README, and `optix pulse --help` no longer imply CLI and HTTP
  `/api/intel/pulse` share auto-view inference. They share the schema, but
  HTTP auto-promotes to `event` on FOMC/CPI days and `shock` on live shock
  regimes via `resolveAutoView`, while the CLI returns only phase views
  (`premarket | intraday | postclose`) from `view_inferred=true`. So a
  snapshot's `view` field is not portable across the two surfaces. Code
  comments at the two auto-view branches cross-reference each other so the
  divergence is discoverable from either side. No behavior change.

## [0.14.23] - 2026-06-16

Patch release closing two small trade-journal / webui contract slips (#162).

### Fixed

- `internal/datastore/sqlite/journal.go` `ListExecutions` and `GetSyncState`
  now route their `time.Parse` through the package's `parseTimeOrLog` helper,
  matching every other read site (the uniformity #44 was filed to enforce).
  Future stored-format drift logs a warning rather than silently producing
  a zero time.
- `internal/webui/server.go` `writeErrorPage` now sets `Content-Type:
  text/html; charset=utf-8` BEFORE `WriteHeader`, mirroring the sibling
  `writeErrorJSON`. Previously the header mutation in `renderPage` ran after
  the status was already committed, so HTML error pages shipped without an
  explicit content type. New regression tests cover both helpers.

## [0.14.22] - 2026-06-16

Patch release for GapFillCard arrow/color consistency.

### Fixed

- GapFillCard headline glyph and color are now driven from the same source
  (`data.direction`). Previously the glyph came from the direction string
  (`'down'` → ↓, anything else → ↑) and the color from the sign of
  `implied_gap_pct`, so a sub-threshold negative gap (backend sets
  `direction=''` when `|gapPct| < 0.25%`) rendered as ↑ in red — glyph and
  color visibly contradicted. Empty direction now renders as a neutral
  → in zinc, matching the "flat" convention from `IntelJournalPanel`.
  ([#175](https://github.com/IS908/optix/issues/175))

## [0.14.21] - 2026-06-15

Patch release for README / SKILL.md command-name correctness.

### Fixed

- README command table and `skills/commands/optix/SKILL.md` no longer reference
  the non-existent `optix intel state` and `optix intel journal` subcommands.
  Replace with the actually-registered `intel status` and
  `intel read|narrative|judge|reconcile`. Both old names produced unhelpful
  parent-help output instead of the documented behavior. ([#161](https://github.com/IS908/optix/issues/161))

## [0.14.20] - 2026-06-15

Patch release fixing three Market Intel logic bugs (#174).

### Fixed

- Postclose timeline now keeps the newest 16 events instead of the oldest 16
  (sort descending before truncating), so today's after-hours movers and
  read-across edges — the whole point of a "what just happened" view — always
  survive when older earnings rows push total events past the cap. The SPA
  card also leads with today's events as a side-effect, since it renders
  `events.slice(0, 8)`. ([#174.1](https://github.com/IS908/optix/issues/174))
- Postclose earnings `Stale` flag is now reachable. The inclusion window's
  lower bound (`now-past`, with `past=14d`) previously equalled the hardcoded
  Stale threshold (`now-14d`), so every row that survived the filter was
  newer than the threshold and the flag always serialized `false`. Add a
  `staleAfter` parameter to `BuildEarningsReports`, widen the production
  window to 30 days, and keep the 14-day Stale threshold. Rows between -30d
  and -14d now appear with `Stale=true`. ([#174.2](https://github.com/IS908/optix/issues/174))
- Judgment-journal hit-rate no longer scores judgments the agent explicitly
  retracted. When a later judgment carries `Supersedes=<earlier_id>`, the
  earlier judgment is now excluded from the hit/miss denominator (its
  individual `Reconciliation` row is still attached for forensics — only the
  aggregate metric changes). ([#174.3](https://github.com/IS908/optix/issues/174))

## [0.14.19] - 2026-06-15

Patch release fixing an inverted risk-tolerance dial in sell-side strike
selection.

### Fixed

- Sell-side strike selection no longer inverts risk tolerance. Previously the
  `delta_target` multiplier was `conservative=0.15 / moderate=0.25 /
  aggressive=0.30`, but the formula uses it as a σ-distance multiplier, so a
  larger value pushes the strike *further* OTM. Conservative sellers were
  therefore handed the strike closest to spot (highest assignment probability)
  while aggressive sellers got the safest one. Conservative now uses 0.30 and
  aggressive 0.15, so the recommended put/call strikes track risk tolerance in
  the expected direction. Unknown risk-tolerance strings now default to
  `moderate` (matching the convention in `_passes_filters`). Updates the user
  manual and three stale docstrings. ([#173](https://github.com/IS908/optix/issues/173))

## [0.14.18] - 2026-06-14

Patch release hardening the IBKR→Yahoo fallback against a process-crashing
reconnect storm.

### Fixed

- Broker pool now applies per-slot exponential backoff (2×health-interval up to
  15 min) before re-probing IBKR for an idle slot stuck on the yfinance
  fallback. Previously every idle fallback slot attempted an IBKR switchback on
  every 30s health cycle; when IBKR was unreachable this storm of connect
  attempts could exhaust TWS's 32-connection limit and repeatedly drive a racy
  connect/teardown path in the `scmhub/ibapi` reader (`panic: sync: negative
  WaitGroup counter`), crashing the whole process instead of degrading to
  delayed data. The backoff throttles the steady-state unreachable-IBKR storm
  (the reported case); the underlying WaitGroup race is an upstream `ibapi`
  defect, so a rapidly flapping gateway can still reach it. ([#171](https://github.com/IS908/optix/issues/171))

## [0.14.17] - 2026-06-14

Patch release for Shock liquidity metric-label correctness.

### Fixed

- Rename Shock liquidity `spread_z` to `spread_ratio` and expose
  `normal_spread_bps`, so API/CLI/WebUI labels describe a multiple of the
  static baseline spread instead of implying a statistical z-score.

## [0.14.16] - 2026-06-14

Patch release for yfinance fallback bar robustness.

### Fixed

- Coerce NaN volume values in yfinance `fetch_bars` to zero via `_safe_int` so
  degraded historical-bar fallback keeps valid bars instead of crashing.

## [0.14.15] - 2026-06-14

Patch release for Market Intel metric-label correctness.

### Fixed

- Label journal hit-rate windows with the queried trading date instead of
  hardcoded `today` for historical reads.
- Stamp judgment reconciliation expiry prices from cached pulse bars as
  `frozen` instead of reusing the registration quote basis.
- Rename Shock fingerprint row `confidence` to `normalized_score` in JSON and
  CLI output, since it is a direct score normalization rather than an
  independent confidence signal.

## [0.14.14] - 2026-06-14

Patch release for Market Intel M4/M7 compute correctness.

### Fixed

- Compute premarket mover percent from today's premarket last price versus the
  latest prior regular-session close instead of a stale multi-day-old bar.
- Compute premarket volume ratio from today's premarket-window volume versus
  prior-day premarket averages instead of a multi-day numerator.
- Keep Shock Regime VIX score contribution and confirmation rows in sync,
  including moderate VIX moves that trigger `watch`.

## [0.14.13] - 2026-06-14

Patch release for Market Intel data-source consistency.

### Fixed

- Deduplicate Shock Intel quote, depth, and option-stress market fetches across
  bundle cards, nested liquidity reads, and the `/api/intel/state` hot path.
- Normalize Market Intel source/basis metadata to canonical marketdata basis
  values and expose delayed postclose row basis in the WebUI.
- Route Event/Shock asset-class lookup and yfinance option-chain fallback
  through shared `marketdata` helpers instead of local duplicated maps/imports.

## [0.14.12] - 2026-06-14

Patch release for Market Intel SPA degraded-data and judgment workflow
contracts.

### Fixed

- Render Market Intel `DataWarnings` in all M4 premarket and M5 postclose
  cards so degraded/partial readings are visible consistently across the SPA.
- Treat nullable premarket/postclose DTO lists as empty degraded data in the
  WebUI instead of relying on Go-side empty-slice JSON invariants.
- Surface the judgment-journal workflow on postclose, event, and shock views
  with view-specific asset chips and `optix intel judge` command context.
- Keep slow-loading intel cards visibly labeled while degraded yfinance-backed
  requests are still pending.

## [0.14.11] - 2026-06-14

Patch release for Market Intel shared helper correctness.

### Fixed

- Share Market Intel symbol normalization across premarket and postclose mover
  universes so watchlist symbols with interior whitespace, such as `BRK B`,
  cannot silently land in different view memberships.
- Share the America/New_York market-clock location through a leaf helper
  package and align the M7 Shock source interface name with the other Intel
  packages' `MarketSource` convention.

## [0.14.10] - 2026-06-14

Patch release for Market Intel correctness, degraded-data contracts, and docs.

### Fixed

- Fix postclose EPS surprise labels when both reported and estimated EPS are
  negative by using absolute estimate as the percentage denominator.
- Treat removed hawkish/dovish FOMC statement sentences as the opposite policy
  signal in Event Diff scoring.
- Align Event Sensitivity drivers by event date instead of slice position so
  missing windows cannot cross-wire assets and macro drivers.
- Keep the premarket gaps card/API on the 200 + warnings contract when the gap
  stats store or refresh path is unavailable, while preserving empty arrays for
  `by_band`.
- Route the agent skill wrapper's Market Intel commands explicitly: `shock`
  probes IBKR and warns on degraded fallback; broker-free Intel commands do not
  require IBKR or Python gRPC.
- Rename mislabeled Market Intel metrics so the Shock regime VIX proxy is
  exposed as `vix_change_ratio`, option stress uses `iv_skew`, and postclose
  read-across exposes `signal_strength` instead of implied hit-rate confidence.
- Sync README Market Intel commands/routes and release-install version guidance,
  and refresh stale CHANGELOG compare links.

## [0.14.9] - 2026-06-14

Patch release for Market Intel roadmap documentation sync.

### Fixed

- Sync Market Intel M4-M7 roadmap/status docs with shipped releases and mark
  the M7 implementation plan release step complete.

## [0.14.8] - 2026-06-14

Patch release for Market Intel NYSE calendar freshness.

### Fixed

- Extend the built-in NYSE trading calendar through 2028 so Market Intel state
  does not raise `calendar_stale` during normal 2028 sessions and correctly
  handles 2028 early closes.

## [0.14.7] - 2026-06-14

Patch release for Market Intel M1 Pulse source transparency.

### Added

- Add per-asset `source` and `basis_note` fields to Market Intel M1 Pulse
  API/CLI output and surface them in the WebUI pulse cards, making yfinance
  delayed/frozen rows and yield/SOX proxy approximations explicit.

## [0.14.6] - 2026-06-14

Patch release for Market Intel M6 event-source upgrades.

### Added

- Add Fed.gov FOMC calendar and statement fetchers for M6 Event Diff and
  event-window dates, with local fixture fallback and visible warnings.
- Add a BLS CPI release schedule fetcher for CPI event dates, with local CPI
  calendar fallback when BLS blocks or fails.

### Fixed

- Preserve abbreviations such as `U.S.` and FOMC member initials when splitting
  official Fed statements into sentences for deterministic diffs.

## [0.14.5] - 2026-06-14

Patch release for Market Intel Intraday placeholder cleanup.

### Fixed

- Remove unsupported Intraday placeholder cards for `盘中异动` and `板块热力`
  from the WebUI until real data-backed cards are implemented.
- Keep postclose movers stable when the source returns no ranked movers by
  serializing empty mover lists as arrays and treating null lists as empty in
  the WebUI.

## [0.14.4] - 2026-06-14

Patch release for Market Intel view override UX.

### Added

- Add an API-level Market Intel view resolver that can auto-route to the Event
  view on scheduled event dates and the Shock view when the M7 regime trigger
  enters shock/critical state.
- Show automatic Event/Shock trigger reasons in the WebUI tabs while preserving
  explicit manual tab selection and the return-to-auto control.

## [0.14.3] - 2026-06-14

Patch release for Market Intel M7 option stress.

### Added

- Add M7 Shock option-stress metrics from broker/yfinance option chains for
  SPY/QQQ/IWM/VIXY ETF proxies, including source/basis/as_of labels and
  put-call ATM IV skew evidence in the Shock fingerprint card.

### Fixed

- Contain long degraded-data warning text in WebUI cards so IBKR/Yahoo fallback
  errors do not overflow the layout.

## [0.14.2] - 2026-06-13

Patch release for Market Intel M7 IBKR shock market depth.

### Added

- Add an IBKR SMART market-depth adapter for M7 Shock liquidity so broker
  market depth can fill top bid/ask levels when IBKR depth data is available.

### Fixed

- Cap M7 fallback quote fetches so rate-limited yfinance or broker fallback
  paths degrade into visible warnings instead of leaving Shock cards loading.

## [0.14.1] - 2026-06-13

Patch release for Market Intel data transparency and degraded-data behavior.

### Fixed

- Surface Market Intel Event/Shock data-source warnings in the WebUI and keep
  M7 IBKR quote fallback warnings visible when yfinance fallback data is used.
- Cap M7 shock broker quote overlays when fallback quotes are already available
  so the Shock view degrades quickly instead of staying in loading state.

## [0.14.0] - 2026-06-13

Minor release: Market Intel M7 — the shock view of epic #120.

### Added

- **Market Intel shock view (`optix shock`, `/intel/` shock cards) — M7 of
  epic #120.** Four source-backed cards: regime trigger scoring, interpretable
  supply/demand/liquidity/policy shock fingerprints, local historical analog
  matching, and ETF liquidity state. New `optix shock` CLI bundle and four
  `GET /api/intel/shock/*` endpoints with explicit source/basis/as_of/warnings
  labels. M7 is IBKR-preferred for ETF top-of-book bid/ask data via the existing
  broker fallback chain, while v1 remains runnable with yfinance macro/quote
  fallback and explicit market-depth/option-stress degradation warnings.

## [0.13.0] - 2026-06-13

Minor release: Market Intel M6 — the event-day view of epic #120.

### Added

- **Market Intel event view (`optix event`, `/intel/` event cards) — M6 of
  epic #120.** Four source-backed cards: rate/yield proxy repricing,
  deterministic FOMC statement wording diff, historical FOMC/CPI event-day
  pattern aggregation, and a signed cross-asset sensitivity matrix. New
  `optix event` CLI bundle and four `GET /api/intel/event/*` endpoints with
  explicit source/basis/as_of/warnings labels. M6 v1 is free-source-first via
  yfinance plus local statement/calendar fixtures; IBKR exact contract mapping
  is left as a future adapter.

## [0.12.1] - 2026-06-13

Patch release for Market Intel M5 postclose cleanup.

### Fixed

- Reused the already computed postclose movers when building timeline and
  bundled read-across output, avoiding duplicate yfinance bar fetches for the
  same request.
- Marked the tracked M5 implementation plan checklist as completed so release
  review no longer reads as unfinished work.

## [0.12.0] - 2026-06-13

Minor release: Market Intel M5 — the postclose view of epic #120.

### Added

- **Market Intel postclose view (`optix postclose`, `/intel/` postclose cards)
  — M5 of epic #120.** Four pure-compute cards: earnings quick read vs free
  yfinance EPS consensus, structured postclose timeline, same-sector
  read-across edges from the embedded sector map, and combined regular-session
  plus after-hours movers. New `optix postclose` CLI bundle (agent 16:30
  checkpoint context) and four `GET /api/intel/postclose/*` endpoints with
  independent per-card failure isolation. No IBKR and no Python gRPC engine.

## [0.11.0] - 2026-06-13

Minor release: Market Intel M4 — the premarket view of epic #120.

### Added

- **Market Intel premarket view (`optix premarket`, `/intel/` premarket cards)
  — M4 of epic #120.** Four pure-compute cards: overnight transmission chain
  (N225→TSMC→SX5E→ES, descriptive relay consistency), implied open + historical
  gap-fill statistics for SPX (migration 007, lazy TTL refresh), premarket
  movers + volume ratio (watchlist ∪ a built-in liquid set), and sentiment
  positioning (Put/Call from the option chain + VIX term premium, degraded).
  New `optix premarket` CLI bundle (agent premarket context at the 08:00
  checkpoint) and four `GET /api/intel/premarket/*` endpoints with independent
  per-card failure isolation. No IBKR and no Python gRPC engine.

## [0.10.0] - 2026-06-13

Minor release: Market Intel M3 — the judgment-journal closed loop (the
narrative plane of epic #120).

### Added

- **Market Intel judgment journal (`optix intel`) — M3 of epic #120.** The
  narrative plane of the three-layer architecture: at daily checkpoints
  (08:00 剧本 / 10:30 首验 / 15:00 定调 / 16:30 对账) an agent writes
  narrative prose and registers falsifiable directional judgments via
  `optix intel {narrative,judge}`; optix captures the registration price,
  settles expired judgments against market price history
  (`optix intel reconcile`), and tracks hit-rate — all pure compute, zero
  LLM. New `GET /api/intel/journal` read endpoint and an SPA narrative panel
  (`/intel/` intraday view) render the stored journal. Three append-only
  SQLite tables (migration 006); writes go through the CLI, the server reads,
  concurrency via SQLite WAL. `optix intel` needs no IBKR and no Python gRPC
  engine.

## [0.9.0] - 2026-06-13

Minor release: Market Intel M2 — embedded SPA skeleton + four-phase market
clock (epic #120).

### Added

- **Market Intel SPA (`/intel/`) — M2 of epic #120.** New `web/` Vite + React
  dashboard embedded into `optix-server` via go:embed: real-time pulse bar
  (30s polling with exponential backoff), phase-following view tabs
  (premarket/intraday/postclose auto + event/shock manual), and slot-grid
  skeletons for the M3–M7 view components. Pure-Go builds keep working
  without node — `/intel/` falls back to a placeholder page; `make build-full`
  produces the full binary.
- **`internal/intel` scheduling plane.** Four-phase market clock
  (premarket/intraday/postclose/closed) with a built-in NYSE 2026–2027
  holiday/early-close calendar, `GET /api/intel/state` (phase, next
  transition, calendar staleness) and `GET /api/intel/pulse` (same JSON
  contract as `optix pulse --format json`).

### Changed

- **`optix pulse` off-hours inference now maps to `postclose`** (last
  session's frozen snapshot) instead of `premarket`. Overnight, weekends and
  NYSE holidays are now `closed` phase; JSON fields are unchanged.
- Pulse snapshots built under an expired context are re-dispatched for
  healthy singleflight waiters (server-embedding hardening).

## [0.8.0] - 2026-06-12

Minor release: Market Intel M1 — the data foundation for the phase-view
dashboard (epic #120).

### Added

- **`optix pulse` — multi-asset market snapshot (Market Intel M1, #121).**
  New `internal/marketdata` package: business-ID asset refs (SPX/ES/US10Y)
  routed by class to pluggable sources (free yfinance to start — including
  Yahoo's free delayed CME futures `ES=F` family, yield indices (`^TNX`
  family quoted by Yahoo as direct percent — no scaling), and the VIX index
  family), batch-first fetching (one Python subprocess for
  N symbols), two-tier caching (60s memory TTL + SQLite `market_pulse_bars`
  with 2-day rolling prune, migration 005). Per-view compositions
  (premarket/intraday/postclose/event/shock) with honest `basis` labeling
  (delayed/approx/frozen) and absent-not-error semantics (`missing[]` +
  `warnings[]`). The first command whose code path never touches IBKR; view clock-inferred
  (America/New_York, DST-safe). Foundation for the Market Intel phase-view
  dashboard (epic #120).

## [0.7.25] - 2026-06-01

Patch release for agent-friendly CLI output.

### Added

- Added agent-friendly JSON output for `quote`, `chain`, `dashboard`,
  `positions`, and `trades`, plus stdout-only `--json -` support for portfolio
  risk reports (#118).
- Expanded the Optix skill descriptor with structured-output guidance,
  dashboard/watchlist examples, journal decision flow, portfolio stdout JSON
  examples, and IBKR host/port override notes (#118).

### Fixed

- Moved non-fatal market-data cache and OI fallback warnings to stderr so they
  cannot corrupt JSON stdout consumed by agents (#118).

## [0.7.24] - 2026-06-01

Patch release for Optix skill descriptor drift cleanup.

### Fixed

- Refreshed the Optix skill frontmatter so agent discovery explicitly covers
  Max Pain, dashboard summaries, trade-journal review, portfolio Greeks,
  stress tests, and Chinese-language risk/position requests.
- Added missing skill command examples for analysis sizing/risk flags, journal
  filters, journal `--no-sync`, and portfolio risk flags.
- Corrected the release-bundle fallback instructions in the skill wrapper and
  removed the stale "future" wording from the release-mode install docs.

## [0.7.23] - 2026-06-01

Patch release for post-release dry-run cleanup.

### Fixed

- Updated developer setup docs to install Python dev extras, matching the
  documented Ruff workflow, and added a `make lint-python` target for the
  check.
- Refreshed the release install example and Go prerequisite in the README.
- Added regression coverage that portfolio stress fallback legs preserve Vega
  in USD per +1% IV, matching the aggregate Greeks convention.

## [0.7.22] - 2026-06-01

Patch release for Gateway/TWS port consistency.

### Fixed

- Updated scheduler integration tests to default to the documented Gateway
  live port (`OPTIX_IB_PORT=gateway` -> `4001`) while still accepting `tws` or
  numeric port overrides (#111).
- Fixed SQLite in-memory stores so pooled test connections share one migrated
  schema, preventing scheduler integration tests from seeing empty databases.
- Aligned test docs and Web UI help text with the four supported IBKR API
  ports: Gateway live/paper `4001`/`4002`, TWS live/paper `7496`/`7497`
  (#111).

## [0.7.21] - 2026-06-01

Patch release for quieter IBKR CLI output.

### Fixed

- Suppressed verbose `github.com/scmhub/ibapi` protocol callback logs by
  default so read-only CLI commands no longer fill stderr with handshake,
  market-data, and tick event internals (#110).
- Stopped intentional `Disconnect()` calls from being reported as TCP drops or
  marking the slot unhealthy while preserving the warning for unexpected IBKR
  connection closures (#110).

## [0.7.20] - 2026-06-01

Patch release for documentation drift cleanup.

### Fixed

- Corrected current ClientID comments and test labels so they no longer claim
  `trades` uses ClientID 5 or that ClientID 6 belongs to `analyze --with-oi`;
  `trades` shares ClientID 0 with journal for cross-client execution
  visibility, while portfolio owns ClientID 5 and watchlist analysis owns
  ClientID 6 (#104).

## [0.7.19] - 2026-06-01

Patch release for the root config-file contract.

### Fixed

- Wired the root `--config` contract for the currently supported global
  settings: `database.path`, `ibkr.host`, `ibkr.port`, and
  `grpc.python_server_addr`. CLI flags still take precedence when provided
  (#102).
- Added `configs/optix.yaml.example` so the documented setup copy command works
  again, and marked unconsumed config keys as reserved instead of active (#102).

## [0.7.18] - 2026-06-01

Patch release for portfolio config authority.

### Fixed

- Wire `portfolio concentration` to `configs/portfolio.yaml` so
  `concentration:` thresholds and `sectors_file` are consumed, while valid
  explicit CLI flags continue to take precedence (#103).
- Wire `portfolio greeks` to `configs/portfolio.yaml` for `greeks.risk_free_rate`
  and `sectors_file`, again preserving explicit flag precedence (#103).
- Preserve explicit zero risk-free rates from YAML or `--risk-free-rate 0` for
  greeks/stress instead of treating them as unset (#103).
- Correct the `iv_staleness:` YAML comments to mark those knobs as parsed but
  reserved until an IV cache freshness gate exists (#103).

## [0.7.17] - 2026-06-01

Patch release for portfolio risk-report consistency.

### Fixed

- Keep `portfolio greeks` market-value and weight calculations aligned with
  `portfolio concentration` when an option leg has market value but cannot
  resolve IV: the leg is still counted in `MVUsd`/weight while Greek
  sensitivities remain skipped (#101).
- Skip and surface missing-mark stock legs in `portfolio greeks` instead of
  reporting nonzero share delta alongside zero market value and zero dollar
  delta (#101).

## [0.7.16] - 2026-06-01

Patch release for trade-journal currency correctness.

### Fixed

- Persist IBKR execution currency in the trade journal and propagate it through
  FIFO round-trip matching so same-symbol fills in different currencies no
  longer match into one synthetic trade (#100).
- Keep review P&L summaries USD-only until FX conversion is implemented:
  non-USD round trips are excluded from realized P&L/win-rate aggregates and
  surfaced via `excluded_non_usd_round_trips`; CLI and Web trip tables now show
  each round trip's currency (#100).
- Backfill migrated duplicate execution rows when a later IBKR sync provides an
  explicit currency, while preserving `UpsertExecutions`' newly-inserted row
  count contract (#100).

## [0.7.15] - 2026-06-01

Patch release for post-review correctness and release-install hardening.

### Fixed

- Correct portfolio option-leg Vega scaling so Greeks output and stress Taylor
  fallback use USD per +1 IV point instead of values scaled down by 100x
  (#95).
- Harden release-mode skill installation so a failed `optix_engine` install or
  broken Python runtime import fails verification instead of reporting success
  (#96).
- Preserve interrupted CLI exit status after cleanup: SIGINT exits 130 and
  SIGTERM exits 143 instead of success (#97).
- Avoid long serial repricing timeout cascades by switching remaining option
  legs in a scenario to Taylor fallback after the first repricing RPC error
  (#98).

## [0.7.14] - 2026-06-01

Patch release for portfolio stress repricing fidelity.

### Fixed

- **`portfolio stress` now reprices option legs for shocked scenarios (#73).**
  Stress reports built from live portfolio Greeks carry JSON-hidden stock and
  option leg inputs, then use the analysis engine to reprice option legs at
  shocked spot/IV values. The previous Delta/Gamma/Vega Taylor approximation
  remains as a fallback when repricing cannot be completed.

## [0.7.13] - 2026-06-01

Patch release for portfolio stress beta freshness.

### Fixed

- **`portfolio stress` now prefers historical per-symbol beta over static
  fallback values (#86).** Stress runs compute beta from the latest aligned
  daily returns versus SPY, cache the result in SQLite with observations and
  freshness metadata, and include beta sources in JSON reports so broad-index
  shocks show whether they used fresh cache, newly computed history, or static
  fallback values.

## [0.7.12] - 2026-06-01

Patch release for IBKR quote day-change correctness.

### Fixed

- **IBKR stock quotes now populate day change and day-change percentage
  (#67).** When IBKR supplies both latest price and prior close ticks,
  including delayed-market-data ticks, `GetQuote` fills `Change` and
  `ChangePct` from `last - close`, so quote, analyze, and dashboard displays
  no longer show a misleading `0.00%` change for every IBKR-sourced symbol.
  Historical fallback quotes also compare the latest daily close against the
  previous daily close instead of reporting zero change.

## [0.7.11] - 2026-06-01

Patch release for the option-chain CLI.

### Fixed

- **`optix chain` now fetches and renders real option-chain data (#77).** The
  command no longer prints a stale "not implemented" success message; it uses
  the existing IBKR/Yahoo Finance fallback broker path, supports
  `--expiry YYYY-MM-DD`, and reports missing expirations with suggestions.

## [0.7.10] - 2026-06-01

Patch release for release workflow Node 24 compatibility.

### Fixed

- **The release workflow now opts into Node 24-compatible GitHub Actions
  (#82).** The workflow upgrades checkout, setup-go, artifact upload/download,
  and GitHub Release publishing actions to Node 24-capable majors and sets the
  Node 24 opt-in environment flag so release runs surface runtime regressions
  before GitHub's default migration window.

## [0.7.9] - 2026-06-01

Patch release for CLI exit-code reliability.

### Fixed

- **CLI runtime failures now preserve documented exit-code categories (#70).**
  `quote`, `analyze`, `dashboard`, `positions`, `trades`, and `max-pain` now
  map SQLite failures to exit code `3` and broker/account-data failures to exit
  code `2` instead of collapsing everything to generic exit code `1`;
  `watch` and `server` now also map SQLite failures to exit code `3`. The
  max-pain skill docs now correctly reserve exit code `3` for SQLite failures
  rather than analysis-engine connectivity.

## [0.7.8] - 2026-06-01

Patch release for portfolio config validation.

### Fixed

- **Portfolio config parsing now rejects duplicate stress scenario IDs,
  duplicate top-level sections, and tab indentation (#74).** These cases now
  fail with explicit errors instead of silently overwriting config or producing
  misleading unknown-key messages.

## [0.7.7] - 2026-06-01

Patch release for portfolio stress index-shock semantics.

### Fixed

- **Market-index stress shocks no longer treat every group as beta=1 or apply
  QQQ shocks to the whole portfolio (#71).** `spy_pct` shocks now scale
  price-sensitive P&L by a built-in beta fallback for common names, while
  `qqq_pct` shocks are restricted to Nasdaq/tech-like tickers and groups.

## [0.7.6] - 2026-06-01

Patch release for portfolio stress transparency.

### Fixed

- **`portfolio stress` now carries and renders skipped option legs (#72).**
  Stress reports include `skipped_leg_count`/`skipped_legs`, and text output
  warns that stress may understate risk when Greeks excluded legs due to missing
  IV, marks, pricing errors, or non-finite values.

## [0.7.5] - 2026-06-01

Patch release for IBKR OI-chain spot correctness.

### Fixed

- **IBKR OI-chain median-strike fallback no longer fabricates authoritative
  underlying prices (#76).** When spot quote lookup fails, the median strike is
  used only to bound the OI fetch window; `UnderlyingPrice` remains unset so
  downstream backfill/caching paths do not persist a plausible but fake spot.

## [0.7.4] - 2026-06-01

Patch release for implied-volatility solver correctness.

### Fixed

- **Degenerate mark-to-IV inversion no longer reports non-finite Newton
  iterates as converged (#75).** The Python solver now requires Newton
  convergence to produce a finite IV inside the supported range before it can
  return success; bad iterates fall through to the bounded Brent fallback.

## [0.7.3] - 2026-06-01

Patch release for IBKR tick-accumulator concurrency.

### Fixed

- **IBKR quote and option-OI tick accumulators are now race-free (#66).**
  Market-data callbacks update quote/OI fields through mutex-protected setters,
  and quote/OI callers read immutable snapshots, including timeout paths where
  queued IBKR callbacks may still arrive after cancellation.

## [0.7.2] - 2026-06-01

Patch release for analysis-engine availability failures.

### Fixed

- **Analysis gRPC calls no longer wait forever when the Python engine is down
  (#68).** The analysis client now applies a default 30s RPC deadline when the
  caller did not provide one, while preserving explicit caller deadlines for
  longer-running analyze/dashboard paths.

## [0.7.1] - 2026-06-01

Patch release for v0.7.0 release-bundle and skill invocation regressions.

### Fixed

- **Release tarballs now ship runtime configs (#65).** The release builder now
  includes `configs/portfolio.yaml` and `configs/sectors.json`, and the
  release-mode installer copies them into `.runtime/configs/` so documented
  default config paths are available after install.

- **The Optix skill now starts the Python analysis engine for all analysis-backed
  commands (#69).** `max-pain`, `portfolio greeks`, and `portfolio stress` now
  use the skill-managed analysis server and injected `--analysis-addr`, matching
  `analyze` and `dashboard`. Skill commands now execute from the runtime root so
  bundled relative defaults like `configs/portfolio.yaml` resolve correctly.

## [0.7.0] - 2026-06-01

Minor release: v2.0 Phase 3 — config-driven portfolio stress scenarios on top
of the v0.6.x portfolio Greeks layer. Adds `optix portfolio stress`, explicit
IV-point shocks, stricter portfolio config validation, and stress docs aligned
with the shipped CLI/config contract.

### Added

- **`optix portfolio stress` — config-driven portfolio stress scenarios (#63).**
  Adds the Phase 3 stress command, reusing the `portfolio greeks` snapshot path
  and applying YAML-configured SPY/QQQ/IV shocks to estimate scenario P&L,
  % NLV, and worst position. Supports `--portfolio-config`, `--net-liq-usd`,
  `--risk-free-rate`, `--sectors-file`, and `--json`.

- **`configs/portfolio.yaml` now carries stress scenarios.** Defaults include
  SPY -3/-5/-10%, IV +5 points, QQQ -5%, and a tech-correlated SPY/IV shock;
  missing/partial config still falls back to built-in defaults.


## [0.6.1] - 2026-05-31

Patch release for portfolio Greeks correctness and latency when IBKR option
position marks are missing. No CLI/API changes.

### Fixed

- **`portfolio greeks` prices missing-mark option legs from OI-chain IV / MarketValue fallback (#61).** When IBKR position marks were missing (`LastPrice == 0`), Greeks previously skipped otherwise valid option legs. The aggregator now prefers the OI-enriched chain path for missing-mark held options so per-contract IV can be used, preserves MV/weight from `Position.MarketValue`, and can derive a per-share mark from `abs(MarketValue)/(abs(qty)*multiplier)` for mark-IV inversion when OI-chain IV is unavailable. The expensive OI path is avoided for fully marked chains. Option chains now backfill missing underlying spot from quote data (Last → bid/ask midpoint → Close), and the IBKR OI path sets `UnderlyingPrice` from the spot quote it already fetched to avoid an extra quote round trip.

## [0.6.0] - 2026-05-31

Minor release: v2.0 Phase 2 — portfolio Greeks aggregation. Adds the
`optix portfolio greeks` command on top of the v0.5.x concentration layer,
reusing the existing Black-Scholes engine rather than building a new
service. No changes to the concentration API.

### Added

- **`optix portfolio greeks` — account-level Greeks aggregation (v2.0 Phase
  2).** Aggregates per-leg option Greeks across all holdings into per-underlying
  / per-sector / total **position-level dollar Greeks**: Net Δ (delta-adjusted
  shares), Dollar Δ (USD per +1% spot), Γ (Δ-shares per +1% spot), Vega (USD per
  +1% IV), Θ (USD per day). IV is taken from the option chain, falling back to
  inverting the held mark (new `AnalysisService.ImpliedVol` RPC); Greeks are
  computed by the Python Black-Scholes engine via the existing `PriceOption`
  RPC. Legs whose IV can't be resolved are skipped and surfaced rather than
  silently producing garbage. `--by underlying|sector`, `--net-liq-usd`,
  `--risk-free-rate`, `--sectors-file`, `--json`. Requires IBKR + the analysis
  engine; reuses the Phase 1 broker (ClientID 5), sector resolution,
  FallbackNLV, and exit-code convention.

### Fixed

- **Skill now surfaces the `portfolio` commands.** `concentration` (shipped in
  v0.5.0) was never listed in SKILL.md, so agents couldn't discover it; added a
  Portfolio Risk section covering both `concentration` and `greeks` plus trigger
  words.

## [0.5.1] - 2026-05-31

Patch release hardening the v0.5.0 portfolio concentration feature: six
post-release bug fixes (#47–#52) plus a whole-branch review pass. None
change the v0.5.0 API; all make the concentration report correct and
trustworthy in edge cases (non-USD holdings, closed-out residuals,
cancelled fetches, cwd-independent sector resolution, ClientID isolation,
and the dual-currency display).

### Fixed

- **`portfolio concentration` fallback NLV now matches `Compute`'s
  inclusion rules (v0.5.1 review pass).** When `--net-liq-usd` was
  omitted, the CLI summed `|MarketValue|` over *all* positions
  (currency-blind) for the denominator, but `Compute` excludes non-USD
  and residual legs from the numerator — so a non-USD holding inflated
  the denominator, broke the documented `deployed_pct == 100%` fallback
  cue, and showed phantom "cash". The shared `portfolio.FallbackNLV`
  helper now applies the same exclusions in both places. Also guards
  against a non-finite (NaN/±Inf) market value, which would have poisoned
  HHI/weights and made the JSON snapshot fail `json.Marshal` — such a
  position is now flagged and contributes 0. `portfolio concentration`
  also follows the project exit-code convention (2 = IBKR unreachable,
  3 = SQLite) so cron consumers can distinguish retryable failures.

- **`portfolio concentration` no longer silently mis-weights non-USD
  holdings (#49).** `positionFromIB` dropped the IBKR contract currency, so
  a position priced in HKD/SGD had its raw market value mixed straight into
  the USD-NLV denominator — wrong by ~8× for an HK name, with no warning.
  `model.Position` now carries `Currency` (populated from `c.Currency`), and
  `Compute` excludes any leg whose currency isn't USD, surfacing them via a
  new `CurrencyMismatchTickers` field and a loud render warning ("Non-USD
  holdings excluded from concentration: 0700 (HKD)") rather than trusting a
  wrong number. An empty currency is treated as USD (older data / fixtures).
  FX conversion is deferred to v2.1; until then exclusion + warning is the
  safe behavior.

- **Cancelled position fetches no longer masquerade as OPRA gaps (#50).**
  When `ctx` was cancelled mid-`enrich`, in-flight goroutines exited without
  populating `MarketValue` but `GetPositions` still returned `(positions,
  nil)` — so a cancelled run rendered as "Option mark missing — needs OPRA"
  instead of surfacing the cancellation. `enrich` now returns `ctx.Err()`
  and `GetPositions` propagates it, so callers exit with a clear
  `context.Canceled` / `deadline exceeded` rather than a misleading partial
  report. (Latent today — no shipped CLI path wraps the context in a
  timeout — but this hardens the account-read path against any future caller
  that does.)

- **`portfolio concentration` uses a distinct IBKR ClientID (#47).** v0.5.0
  reused ClientID 4, which collides with `optix positions` — running the
  two concurrently silently failed the second connection. The command now
  uses a dedicated `portfolioClientID = 5` (the free slot in the matrix),
  pinned by `TestPortfolioClientIDDistinct` so a future subcommand can't
  silently reuse it.

- **`--sectors-file` no longer breaks when run outside the repo (#48).**
  The v0.5.0 default was the repo-relative `./configs/sectors.json`, so
  running the installed binary from any other cwd silently produced an
  all-"unclassified" sector view. Resolution now follows a search chain —
  explicit `--sectors-file` → `$OPTIX_SECTORS_FILE` → `<bin-dir>/../configs/`
  → `./configs/` → an **embedded** default (compiled into the binary via
  `go:embed`, so the sector map is never silently empty). An explicit flag
  or env path that's missing/malformed now fails loudly rather than
  silently falling back. The resolved source is printed to stderr when it
  isn't the most-expected location.

- **`portfolio concentration` now populates the dual-currency display
  block.** New `--net-liq-sgd` and `--fx-usd-sgd` flags wire data into the
  `NetLiqSGD` / `FXUSDtoSGD` fields that the v0.5.0 JSON schema exposed
  but the CLI never set. Validation (`validateCurrencyFlags`) runs up-front
  before any broker work: the two flags must be passed together or neither
  (so partial input doesn't render misleading `USD $X (SGD $0)` output),
  and a negative value is rejected with a sign-specific message rather than
  the confusing "must be passed together". Table-tested across both-set,
  neither, each-alone, and negative inputs. Will become automatic once the
  IBKR account-summary integration ships. (#51)

- **`portfolio concentration` no longer surfaces IBKR's closed-out
  zero-quantity residual rows.** Positions IBKR keeps for ~T+2 after a
  close were rendered as `0.0%` ghost entries in the Top-N table and
  added to sector ticker lists. They're now skipped at the position-loop
  entry, keyed on quantity alone (`abs(qty) < 1e-9`) — a closed position
  is defined by zero quantity regardless of any stale mark IBKR may still
  report for the row, and the epsilon catches float-represented zeros
  without over-filtering legitimate fractional holdings. Short positions
  (negative quantity) are preserved. (#52)

## [0.5.0] - 2026-05-31

### Added

- **`optix portfolio concentration` — account-level concentration analysis
  (v2.0 Phase 1).** New `portfolio` parent command and `concentration`
  subcommand that aggregates per-underlying exposure (stock + option legs by
  |market value|), groups by sector via static map, and flags positions
  exceeding configurable thresholds (default 10% yellow, 20% red). Reports
  Top-2 / Top-5 / Top-N rollups and HHI with diversified/moderate/concentrated
  buckets. Optional `--json` writes the full snapshot for cron consumers.

  Phase 1 caveat: optix does not yet read NLV from IBKR account summary; pass
  `--net-liq-usd` to anchor weights, or omit to fall back to sum(|MV|). True
  NLV integration lands in v2.1 alongside the Greeks aggregation layer.

  See `docs/v2.0-portfolio-risk-layer.md` for the full design rationale and
  Phase 2/3 roadmap (Greeks aggregation + stress test).

- **`configs/sectors.json`** — static ticker → sector mapping covering ~60
  commonly traded US tickers. Unlisted tickers fall back to "unclassified"
  rather than crashing the report.

- **`configs/portfolio.yaml`** — declarative thresholds reference for the
  portfolio subsystem. Phase 1 reads defaults from code; YAML loading lands
  in a follow-up.

- **`AGENTS.md`** — Codex counterpart to `CLAUDE.md`, mirroring the same
  architecture / command / convention guidance for repo-aware agents.

### Internal

- `internal/portfolio/` — new package for account-level risk views. Phase 1
  ships `concentration.go` + tests; Phase 2/3 will add `aggregator.go` /
  `staleness.go` / `stress.go` per the design doc.

- `.gitignore` now excludes `.superpowers/` scratch dir (same rationale as
  the existing `docs/superpowers/{plans,specs}/` ignores — working artifacts,
  not source).

### Fixed

Pre-release review pass on the concentration code surfaced four real defects;
all fixed before tagging v0.5.0:

- **Missing-mark warning now attributes to specific option legs**, not the
  whole ticker. Previously a GOOGL holding with a fully-priced stock leg plus
  one OPRA-less option leg rendered "Mark price missing for: GOOGL", which
  misdirected the user toward the stock. Output now reads "Option mark
  missing on: GOOGL (1 option leg)" and the ticker's stock-leg MV is preserved
  in the weight calc rather than zeroed out.

- **`Compute` with zero-init `Config` no longer red-flags every position.**
  The prior partial-guard only filled `TopN`, so callers passing
  `portfolio.Config{}` got `RedPct=0` and any non-zero weight tripped the red
  threshold. `applyConfigDefaults` now fills each threshold field
  independently from `DefaultConfig`.

- **Deterministic ordering when two underlyings have identical |MV|.** Sort
  comparators (both per-underlying and per-sector) now use an alphabetical
  tiebreaker, so the JSON snapshot is stable across runs and downstream cron
  diffing doesn't flap.

- **Stock legs with `MarketValue == 0` no longer mis-flagged as missing-mark**
  (they're almost always zero-qty residuals). Missing-mark detection is now
  scoped to option legs only, where the OPRA-subscription gap is the actual
  pathology.

Two deeper issues are tracked as TODOs in code and deferred to v2.1 / v2.0.1:
distinguishing legitimate `mark == 0` options from unfetched marks (needs a
broker.AccountReader API change), and explicit "(margin used)" leverage labels
when `DeployedUSD > NLV`.


## [0.4.5] - 2026-05-23

Patch release: two related correctness bugs in the trade journal subsystem
that together prevented meaningful weekly P&L review for any account with
spread trades. Discovered while doing a weekly review of GLW Bull Put
Spread trades — the surface symptom ("realized P&L is wrong") was actually
two independent bugs stacked.

### Fixed

- **BAG combo executions (spreads) now match as a single round trip
  (#29).** IBKR sends three executions per spread trade: one BAG combo
  row (no expiration/strike/right, signed combo price) plus one OPT row
  per leg — all sharing the same PermID. The matcher was grouping by
  SecType, so the three rows fell into three separate buckets and
  produced three independent round trips per spread, with wrong per-trip
  P&L (legs taken in isolation; BAG with multiplier=1 and unhandled
  signed price) plus a phantom "open" BAG trip that never expired. The
  GLW Bull Put Spread case produced 3 trips totalling -$180 plus a
  phantom open; should have been 1 trip @ +$180.

  **Fix (Path A):** group executions by `(Account, PermID)`; when a
  group contains a BAG row, drop the OPT legs from matching and inherit
  `max(legs.expiration)` onto the BAG (supports vertical, IC, butterfly,
  and calendar spreads). Normalize the BAG's signed price to its
  absolute value — IBKR's `Side` (SLD/BOT) stays authoritative for
  open/close direction; the price sign is informational. Extend
  `multiplierFor` and `emitOpen`'s expired-conversion logic to treat BAG
  identically to OPT. Account-keyed grouping prevents silent
  cross-account merging if two accounts ever produce the same numeric
  PermID. Single-leg OPT and STK paths are untouched — Path A's
  discipline is verified by 8 pre-existing matcher tests passing
  unchanged. 14 new tests cover the GLW reproduction (T1), BTC closes
  (T2), iron condor (T3), calendar fully-expired (T4), calendar
  mid-state (T5), solo OPT regression (T6), STK regression (T7),
  BAG+OPT coexistence (T8), cross-account isolation, plus a 4-row truth
  table for the price normalization invariant.

- **`journal review` and `journal trips` time windows now anchor on
  round-trip CloseTime (#29 follow-up).** Both commands previously
  filtered executions by time *before* feeding the matcher, so a trip
  opened before the window but closed inside (e.g. a put sold last week
  that expired this week) appeared as a dangling close fill with no
  prior open — the matcher emitted a phantom "open" trip and the
  realized P&L was silently dropped from the weekly aggregate.

  **Fix:** load all executions (filtered only by `--symbol` when set),
  run the matcher on the full history, then filter resulting trips by
  inclusive `CloseTime ∈ [since, until]`. Callers (CLI and webui)
  pre-adjust `--until YYYY-MM-DD` to end-of-day (`t.Add(24h - 1s)`) so
  the inclusive upper bound covers the whole day the user typed. Open
  trips (zero CloseTime) are skipped in windowed queries; callers
  wanting open trips query without `--since/--until` or pass
  `--status open`. `Review`'s `TotalExecutions` count intentionally
  still uses the raw-execution window (it answers "how many fills this
  week", distinct from "how many positions realized P&L"); the
  divergence is documented in code. CLI help text for `journal trips`
  and `journal review` `--since`/`--until` updated to reflect the new
  anchor ("Earliest/Latest close date YYYY-MM-DD (filters by trip
  CloseTime)"). 6 new R-tests pin the open-outside/close-inside
  scenario, both-inside case, open-trip skip, boundary-inclusive
  semantics, multi-window isolation, and an off-by-one regression
  guard.

## [0.4.4] - 2026-05-23

Patch release: two more silent-failure fixes continuing the v0.4.2 / v0.4.3
audit. The first is the most impactful since v0.4.0 — `optix journal sync`
and `optix trades` were effectively dead on default TWS configurations,
which explains why the journal feature looked sparse in real-world use.

### Fixed
- **`journal sync` and `trades` returned 0 fills on default TWS configs
  (#30, #43).** IBKR's `reqExecutions` returns only the connecting
  client's own fills *unless* the connecting ClientID matches TWS's
  "Master API Client ID" setting. Since optix is read-only and never
  places orders, the journal client (ClientID 7) and trades client
  (ClientID 5) silently saw the empty set regardless of how much the
  user traded in TWS. ClientID 0 is IBKR's *implicit* master and gets
  cross-client execution visibility without requiring users to configure
  TWS. Change `journalClientID` from 7 → 0 and extract a new
  `tradesClientID = 0` const replacing the inline `ClientID: 5`. Other
  subcommands keep their non-zero IDs since they call account/market
  APIs (`reqMktData`, `reqPositions`, etc.) that aren't gated on master
  status. New `TestExecutionsReaderClientIDsAreMaster` pins both
  constants at 0 with a docstring tying the assertion back to #30 so a
  future refactor can't silently regress.

  **Caveat (documented in const docstrings):** users who have explicitly
  set TWS → API → Master API Client ID to a different non-zero value
  will still hit the original bug. No CLI override exists today — file
  an issue if you need it.

- **sqlite read paths silently dropped `time.Parse` errors (#44, #45).**
  Seven read sites (`GetAnalysisCache`, `GetBackgroundJob` ×3 fields,
  `GetBackgroundJobsForSymbol` ×3 fields, `GetRecentFailures` ×3 fields)
  used `t, _ := time.Parse(time.RFC3339, ...)` and returned the zero
  `time.Time` on parse failure. Under steady state this works (writers
  format RFC3339), but the first writer/reader format drift (e.g. a
  future writer switching to `RFC3339Nano`) would silently zero every
  read and downstream code would mistake the zero for "no value." New
  `parseTimeOrLog(s, fieldCtx) time.Time` helper: empty string returns
  zero quietly (legitimate "no value"), parse failure returns zero AND
  logs the field context + offending value so the next stale read shows
  up in operator logs. Same silent-failure family as #27 / #31 / #37 /
  #41.

  Twelve other `time.Parse` sites in `sqlite.go` (freshness/cache
  markers — `*At` fields) are out of scope per issue #44's deliberate
  enumeration: their zero values render gracefully as "(never)" in user
  output, not silent breaks.

### Notes
- Both fixes are independent (zero file overlap); merge order was
  immaterial.
- No `placeOrder` / `cancelOrder` / `modifyOrder` introduced —
  read-only IBKR boundary preserved.
- The v0.4.2 → v0.4.4 silent-failure audit has shipped **9 fixes
  total** across IBKR, yfinance, scheduler, webui, server, broker, and
  sqlite. Worth a follow-up sweep to catch any remaining
  `_ = err` / `t, _ := time.Parse(...)` patterns in less-trafficked
  code paths.

## [0.4.3] - 2026-05-23

Patch release: three silent-failure bugs caught by the systematic review
pass that produced v0.4.2. Same shape as that batch — no new functionality,
no schema changes, fixes are minimal + test-pinned. Two cover broker /
infra (cancellation + cache), one covers the webui handler error path.

### Fixed
- **Web UI silently dropped per-symbol `UpdateWatchlistConfig` errors (#37,
  #38).** `handleWatchlistAdd` had a `_ = err` swallow paired with a comment
  claiming "log error" — the log was never written. Symbols stayed in the
  watchlist without their intended auto-refresh setting and nothing
  surfaced the failure. Extracted `applyWatchlistConfig(store, symbols,
  autoRefresh, interval) map[string]error` (testable via a 1-method
  interface), and the handler now `log.Printf`s each per-symbol error. A
  test pins "does not stop on first error" so a future early-return
  refactor can't silently regress.
- **`MarketDataService.GetHistoricalBars` cache predicate never held (#39,
  #40).** The cache short-circuit was gated on `len(bars) >= days`, but
  `store.GetBars` is a `LIMIT ?` query and US markets only have ~252
  trading days per year. The canonical 365-day request could never satisfy
  the predicate — so every `analyze` / `dashboard` / scheduler call
  bypassed the cache and fanned out to IBKR / yfinance, masking a
  silent perf regression. The downstream `>= 20` floor in
  `fetchSymbolDataInternal` already handled the degenerate "few cached
  bars" case, so the fix is simply `len(bars) > 0` + keep the 48h
  freshness check as the meaningful guard. Tests pin fresh-hit /
  stale-fetch / broker-error-fall-back-to-stale paths with a
  call-counting fake broker.
- **`FallbackBroker.Connect` treated `context.Canceled` as "IBKR
  unreachable" (#41, #42).** Any primary error fell through to the yfinance
  fallback unconditionally. Since `yfinance.Connect` is a no-op that
  ignores ctx, a user's Ctrl-C during a slow IBKR dial was silently
  reinterpreted as "please use delayed data." Add a `ctx.Err()` check
  between primary failure and fallback `Connect` so both
  `context.Canceled` and `context.DeadlineExceeded` propagate. First test
  file in `internal/broker/` — 3 subtests pin the contract, including
  asserting the fallback is *not* invoked on a pre-cancelled ctx.

### Notes
- All three fixes are independent (zero file overlap); merge order was
  immaterial. Continues the silent-failure-audit theme of v0.4.2.
- Hard scope boundary preserved — no `placeOrder` / `cancelOrder` /
  `modifyOrder` introduced.
- Out-of-scope finding from #42 worth a follow-up: two outdated comments
  in `fallback.go` claim yfinance lacks option-chain / OI support, but
  yfinance has implemented `OIFetcher` since v0.4.0. Cosmetic but worth
  a small doc PR.

## [0.4.2] - 2026-05-23

Patch release: four root-cause fixes for production issues observed against
live IBKR + Yahoo Finance. No new functionality; no schema changes; no
behavior changes on the happy paths.

### Fixed
- **IBKR `Execution.time` IANA-tz parse failure (#27, #33).** `parseExecTime`
  only handled three timestamp layouts; newer `scmhub/ibapi` versions deliver
  the field with a trailing IANA timezone (e.g. `20260520 14:30:21
  America/New_York`). None of the existing layouts matched, the function
  silently returned `time.Time{}`, and downstream `journal list --since` /
  `journal trips` consumed the zero-time as "year 1 AD" — surfaced as
  ~106,000-day holding periods. Strip IANA suffix, resolve via
  `time.LoadLocation`, parse with `ParseInLocation`. Log on any
  no-layout-match so the next IBKR format change shows in logs instead of
  rotting silently. Backwards-compatible with the old (no-tz) format.
- **IBKR wrapper double-close panic (#28, #34).** `wrapper.go`'s Error
  handler and the four `*End` callbacks (`HistoricalDataEnd`,
  `ContractDetailsEnd`, `ExecDetailsEnd`,
  `SecurityDefinitionOptionParameterEnd`) both closed the request's `done`
  channel. On the End-after-Error sequence — realistic from IBKR's API on
  partial-data + error responses — End would close an already-closed
  channel and panic the whole process. `pendingQuote` and `pendingOI` were
  already protected by `sync.Once`; this brings `pendingBars`,
  `pendingOptParams`, `pendingContractDetails`, and `pendingExecutions`
  to parity. Regression test wraps all four types in a `recover()`-guarded
  table.
- **yfinance silent data loss on unparseable timestamp (#31, #35).**
  `GetHistoricalBars` swallowed `time.Parse` errors with `_, _ :=` and
  appended a bar with `Timestamp = time.Time{}`. Downstream,
  `sqlite.InsertBars` derives the dedup key from
  `Timestamp.UTC().Truncate(24h).Format(RFC3339)` — every bad bar collapses
  onto `0001-01-01T00:00:00Z` and `INSERT OR REPLACE` overwrites earlier
  bad rows *across symbols*. Extract bar parsing into a pure
  `parseBarsJSON([]byte) ([]model.OHLCV, error)` (testable without the
  Python subprocess), drop bars with unparseable timestamps, and log the
  miss.
- **Scheduler worker hang on shutdown (#32, #36).** `worker.Run()` used a
  bare `time.Sleep(w.throttle)` between tasks; the outer select honored
  `ctx.Done()` while waiting for a task, but during the post-task throttle
  (default 12s) cancellation was invisible. Graceful shutdown hung up to
  12s per worker, 5× amplified at the default fleet size. New
  `waitOrCancel(ctx, d) bool` helper uses `time.NewTimer` +
  `defer timer.Stop()` (no timer leak) and selects between the timer and
  `ctx.Done()`. Workers now exit promptly mid-throttle while preserving
  the throttle behavior on the happy path. Helper-level unit test covers
  the four cases: timer fires, pre-cancelled ctx, cancel mid-wait, and
  `d ≤ 0`.

### Notes
- All four fixes are independent (zero file overlap); merge order was
  immaterial.
- No `placeOrder` / `cancelOrder` / `modifyOrder` paths introduced —
  read-only IBKR boundary preserved.
- Self-review loop: 2 consecutive clean rounds before merge. Each PR's
  test demonstrably fails on its pre-fix code.

## [0.4.1] - 2026-05-20

### Fixed
- `skills/commands/optix/SKILL.md`: corrected the wrapper invocation path
  back to `bin/optix.sh`. A concurrent edit during the v0.4.0 work
  rewrote every `bash bin/optix.sh ...` example to bare `bash optix.sh`,
  but the installed skill layout places the thin wrapper at
  `<skill_dir>/bin/optix.sh` (per `install.sh`'s own layout doc). Agents
  following the v0.4.0 descriptor hit `bash: optix.sh: No such file or
  directory`. (#26)

## [0.4.0] - 2026-05-20

Adds `--expiry YYYY-MM-DD` to `optix analyze --with-oi` and a standalone
`optix max-pain` command, fixing a bug where the broker's *nearest* option
expiration was silently used for Max Pain computation. Users wanting a
specific Friday cycle had no way to override, and the output never disclosed
which expiry was used. (#25)

### Added
- `optix analyze --with-oi --expiry YYYY-MM-DD` — opt-in expiry override.
  Default behaviour (no `--expiry`) unchanged; bad expiries surface a
  closest-first suggestion list with a copy-paste-ready "Did you mean".
- `optix max-pain <SYMBOL> [--expiry] [--source ibkr|yfinance|auto] [--format text|json]`
  — standalone Max Pain query. ClientID 8. JSON output includes a
  `max_pain_offset_pct` field convenient for agents.
- yfinance broker `GetOptionChain` is now functional (previously a stub),
  with an `option_chain` subcommand added to fetcher.py. `yfinance.Client`
  also implements `broker.OIFetcher` (yfinance returns OI inline at no
  extra cost), so `max-pain --source yfinance` works end-to-end without
  IBKR.
- Shared `broker.ErrExpiryNotAvailable` structured error + CLI helper
  `cli.FormatExpiryError` that renders a closest-first suggestion list,
  reused by both `analyze` and `max-pain`.

### Changed
- `optix analyze --with-oi` output always shows the expiry used
  (`Max Pain: $X.XX (expiry YYYY-MM-DD)`), even when no `--expiry` is
  given. Makes the previously-silent "default = nearest" choice visible.
  `, requested` is appended only when the user explicitly chose.
- `internal/broker/factory.go` moved to `internal/broker/factory/factory.go`
  (own subpackage) to break an import cycle introduced by `ibkr` needing
  to reference `*broker.ErrExpiryNotAvailable`. Concrete brokers now
  depend on the abstract `broker` package; the factory composes both.
  Callers: `factory.NewWithFallback(...)` instead of `broker.NewWithFallback(...)`.

### Notes
- `--source auto` (the new `max-pain` default) reuses the existing
  IBKR→Yahoo Finance fallback chain. JSON `source` field resolves to
  the *actual* broker used (`"ibkr"` or `"yfinance"`), not `"auto"`.
- Bad-expiry errors carry exit code 1; IBKR/yfinance unreachable carry 2;
  analysis engine unreachable carries 3 — matching v0.3.0's journal codes.
- Hard scope boundary preserved: zero `placeOrder|cancelOrder|modifyOrder`
  introduced. Optix remains read-only with respect to IBKR.

## [0.3.0] - 2026-05-16

Trade journal: a persistent record of IBKR executions that survives the
broker's 7-day history window, with FIFO round-trip matching and a
retrospective (复盘) summary surface. Also adds an explicit read-only
scope warning to the agent skill descriptor. (#23, #24)

### Added
- `optix journal status` — show journal size, last-sync time, and gap-warning
  state. **Offline-safe** — does NOT contact IBKR. Useful for agents to
  decide whether to call `sync` first.
- `optix journal sync` — pull the last 7 days of executions from IBKR into
  the local journal. Idempotent (`INSERT OR IGNORE` on `exec_id UNIQUE`);
  re-running reports `new_count: 0`. Uses clientID 7 to avoid colliding
  with `positions` (4), `trades` (5), and `analyze --watchlist` (6).
- `optix journal list [--symbol] [--side] [--type] [--since] [--until] [--limit]`
  — list persisted executions. Auto-syncs from IBKR first; `--no-sync`
  skips the IBKR round-trip and reads SQLite only.
- `optix journal trips [--symbol] [--status open|closed|expired] [--since] [--until]`
  — FIFO-matched round trips (LONG/SHORT, scaling in/out, option expiry)
  with realized P&L per trip and holding days. Computed on read from the
  execution log — never materialized.
- `optix journal review [--since] [--until]` — retrospective summary:
  total trades, win rate, total/avg realized P&L, average holding days,
  by-symbol breakdown sorted by P&L descending. Win-rate denominator is
  `closed + expired` (open trips don't count for or against win rate).
- All journal commands support `--format text|json`. JSON shapes are
  documented in [`docs/journal_json_schema.md`](docs/journal_json_schema.md)
  as a stable agent contract; field additions are non-breaking, removals
  require a versioned bump.
- Documented CLI exit codes: 0 success · 1 generic error · 2 IBKR unreachable
  · 3 SQLite read/write failed. Wired via `cli.ExitErr` so agents can
  branch reliably on failure modes.
- Web UI `/journal` page with three tabs (Trades / Round Trips / Review)
  and a "Sync now" button. DOM is constructed via `createElement` +
  `textContent` (no `innerHTML`) for XSS safety. A gap-warning banner
  appears when `last_sync_at` is missing or more than 6 days old —
  distinguishes "never synced" from a stale sync.
- Web UI JSON endpoints: `GET /api/journal{,/trips,/review}` and
  `POST /api/journal/sync` (returns HTTP 502 with `ibkr_ok:false` on
  broker failure).
- `optix-server --journal-sync-interval=6h` background ticker (set `0`
  to disable). Runs an initial catch-up sync at startup, then ticks on
  the interval. Failures are logged and recorded into
  `sync_state.last_error`; they never block the HTTP listener.
- SQLite migration `003_trade_journal.sql`: new `trade_journal` table
  (UNIQUE `exec_id`, ORDER BY time DESC indexes on symbol/time/account)
  and `trade_journal_sync_state` single-row table.
- New SKILL.md top-of-file warning making the **read-only IBKR scope**
  explicit (bilingual EN/中文): the skill cannot place, modify, or
  cancel orders. All trading must be performed in TWS/Gateway directly.

### Changed
- `cli.Execute()` now returns `error` (was `void`). The `cmd/optix-cli`
  and `cmd/optix-server` `main()` functions translate the result via
  `cli.AsExitCode(err)` to honor `ExitErr` codes. Pre-existing commands
  unaffected.
- `webui.NewServer` is unchanged; a new `webui.NewWithJournal(...)`
  constructor accepts `JournalDeps{Service, Store}`. The journal HTTP
  handlers nil-guard the dep and return 503 if absent (defensive).

### Notes
- IBKR-side trading is **out of scope** — Optix is read-only with respect
  to IBKR and this PR introduces no order-placement paths (verified by
  grep: zero matches for `placeOrder|cancelOrder|modifyOrder` across
  `internal/`, `cmd/`, `pkg/`).
- The first `optix journal list --format json` response currently emits
  `Execution` fields in PascalCase (`"Symbol"`, `"ExecID"`, …) because
  the existing `model.Execution` type has no JSON tags. The schema doc
  notes this; adding snake_case tags would be a coordinated breaking
  change across all existing JSON consumers and is deferred to a
  follow-up.

## [0.2.0] - 2026-05-15

Minor release: introduces read-only IBKR account commands (positions
holdings with mark-to-market P&L, and last-7-days execution history),
along with two new optional broker interfaces (`AccountReader`,
`OptionQuoter`) that keep the yfinance fallback path clean. (#21)

### Added
- `optix positions [--type stk|opt]` — current account holdings snapshot from
  IBKR, with mark-to-market P&L. Stocks and options are shown in separate
  sections; stock P&L uses the live quote, option P&L uses the option mark
  (requires an OPRA subscription — without it the option mark/P&L columns
  degrade to "—", identity + cost still render). Option P&L correctly
  applies the contract `Multiplier`; short positions show a properly-signed
  `UnrealPnLPct`. `--type` is case-insensitive (`stk`/`STK`/`Stk` all work).
- `optix trades [--symbol] [--side] [--since]` — execution history for the
  last 7 days (IBKR's `ReqExecutions` window). `--symbol` and `--side` are
  case-insensitive; `--side` accepts `BOT`/`SLD` (filtered client-side, since
  IBKR's `ExecutionFilter.Side` expects `BUY`/`SELL`). `--since` older than
  7 days is clamped with a warning; the clamp boundary is date-aligned at
  UTC midnight so `--since` matching exactly today−7d passes through.
- Optional broker interfaces `AccountReader` and `OptionQuoter` mirroring
  the existing `Pinger`/`OIFetcher` pattern. IBKR implements both; the
  yfinance fallback returns `ErrAccountNotSupported` and the CLI prints a
  clear "需要 IBKR 连接" error with non-zero exit.
- `AccountService` (`internal/server/account_svc.go`) — fetches positions
  and executions and enriches positions with marks (stocks via `GetQuote`,
  options via `GetOptionQuote`) at bounded concurrency (5 concurrent fetches).
  Failed marks degrade gracefully: that position's P&L stays zero, others
  are unaffected.

### Repo hygiene
- `docs/superpowers/{plans,specs}/` is now `.gitignore`d. These are agent
  workflow artifacts (brainstorming → writing-plans output), not maintained
  documentation; the rationale that produced any merged feature lives in
  commit messages and PR descriptions. The 8 pre-existing scaffolding files
  from earlier features were removed in the same change (content preserved
  in git history).

## [0.1.1] - 2026-05-10

Patch release: fixes a regression in v0.1.0's Yahoo Finance fallback path
and adds end-user release-install documentation.

### Fixed
- **Yahoo Finance fallback in release tarballs** — the Go binary located
  `fetcher.py` via `runtime.Caller`, which under `-trimpath` returned a
  module-relative path that Python then resolved against the user's `cwd`
  (not the binary). Result: every `quote` / `analyze` / `dashboard`
  invocation failed with `[Errno 2] No such file or directory:
  '<cwd>/github.com/IS908/optix/internal/broker/yfinance/fetcher.py'`
  whenever IBKR was unreachable. Fixed by embedding `fetcher.py` via
  `//go:embed` and materializing it to a hash-named file under `$TMPDIR`
  on first use — the binary is now fully self-contained. (#19)
- `install.sh` exited with code 1 in release mode despite a successful
  install. The trailing `[[ "$MODE" == "dev" ]] && echo "..."` test, under
  `set -e`, propagated as the script's final exit status. Added `|| true`.

### Documentation
- **`README.md`**: added "Install from a release (recommended for end users)"
  to Quick Start with a complete `curl | tar -xz | install.sh` flow
  (OS/ARCH detection + SHA256SUMS verification); renamed the source-tree
  path to "Build from source (developers)"; updated the Agent Skill Layout
  to reflect the released `.runtime/` design and dropped the "coming soon"
  tag now that v0.1.0 ships.
- **`README_CN.md`** (new): full Chinese translation of the README,
  structurally aligned with the English version. Adds a `make release`
  local dry-run section that the English version doesn't yet cover.
- Cross-links between the two READMEs at the top of each file.

## [0.1.0] - 2026-05-10

First tagged release. Establishes the install/distribution model and consolidates
the IBKR connection-handling work from the preceding PRs.

### Added
- **Cross-platform release tarballs.** `make release VERSION=vX.Y.Z` (and
  `make release-all`) produces self-contained tarballs for
  `darwin-{arm64,amd64}` and `linux-{amd64,arm64}` (~7 MB each). A GitHub
  Actions workflow at `.github/workflows/release.yml` runs the matrix on
  every `v*` tag push and publishes a GitHub Release with all four
  artifacts plus `SHA256SUMS`. Builds use pure-Go cross-compilation
  (`CGO_ENABLED=0` + `modernc.org/sqlite`); no Docker, no qemu. (#17, #18)
- **Self-contained skill bundle** at `~/.agents/skills/optix/`, with a hidden
  `.runtime/` subdirectory holding the binary, Python engine, sqlite cache,
  and orchestration script. Per-agent skill dirs
  (`~/.claude/skills/optix`, `~/.openclaw/skills/optix`,
  `~/.hermes/skills/optix`) become relative symlinks back to the canonical
  bundle, mirroring the lark-* convention. (#15, #16)
- `install.sh` auto-detects two install modes:
  - **dev** (source-tree checkout): `.runtime/` symlinks to the source so
    `make build` edits take effect immediately
  - **release** (extracted tarball): `.runtime/` is a real directory and a
    Python venv is created on the user's machine
- `install.sh` accepts comma-separated agents (`--agent claude,openclaw`),
  auto-detects available agents when omitted, and supports
  `--uninstall` / `--uninstall --purge` from the canonical-installed copy of
  `install.sh` itself (no need to keep the original tarball around).
- `skill-wrapper.sh` resolves the runtime via a three-step chain:
  `$OPTIX_HOME` → `<skill_dir>/.runtime/` → `command -v optix` (PATH).
- `--with-oi` flag for `optix analyze`: fetches per-contract Open Interest
  via streaming `reqMktData` (generic tick `101`) so Max Pain can be
  computed. Bounded scope: nearest expiration only, ±15% strike window
  around spot, max 5 concurrent requests, 5s per-contract timeout.
  Per-contract IB errors (e.g., 10091 missing OPRA subscription) are
  isolated and skipped. (#13)
- `broker.OIFetcher` optional interface; `FallbackBroker` returns
  `ErrOINotSupported` when the active broker is yfinance.
- IBKR connection pool (`internal/webui/broker_pool.go`): 8 slots reusing
  ClientIDs 30–37, with active health-check probing (`ReqCurrentTime`),
  auto-reconnect on TCP drop, and IBKR-recovery switchback while running on
  the yfinance fallback. (#13)
- `singleflight` deduplication for concurrent same-symbol requests in the
  Web UI live-refresh path. (#13)
- `--ib-port` accepts `gateway` / `tws` string aliases in addition to
  numeric ports.
- `CHANGELOG.md` (this file).

### Changed
- Default `--ib-port` is now `4001` (IB Gateway live) instead of `7496`
  (TWS). TWS users override with `--ib-port tws` or `--ib-port 7496`. (#13)
- `install.sh` rewritten — no more per-agent SKILL.md generation; one copy
  of the SKILL descriptor is shared by every supported agent via the
  canonical-bundle symlink chain. (#16)
- The skill wrapper resolves paths relative to `BASH_SOURCE` instead of
  `git rev-parse`, so it works correctly in git worktrees, tarballs without
  `.git`, and any cwd.
- `optix.sh` cleanup trap rewritten to always `return 0` — earlier the
  trailing `[[ -n $READY_FILE ]] && rm` test would corrupt the script's
  exit code under `set -e` and cause every successful command (e.g.
  `watch list`) to surface as exit 1.

### Fixed
- IBKR connection-count exhaustion and ClientID collisions:
  - `analyze` CLI ClientID changed from 5 → 6 (was colliding with the Web UI)
  - Handshake timeout / ctx-cancel paths now explicitly `Disconnect()` with
    diagnostic logs
  - `ContractDetails` error path closes its `done` channel (was leaking a
    goroutine on every option-chain error)
  - `FallbackBroker.Connect` explicitly disconnects the primary on failure
    before activating the secondary
- TWS TCP-drop detection: `IbWrapper.ConnectionClosed()` now flips
  `Client.connected = false` immediately, surfacing dead connections to the
  pool's health checker on the next probe cycle.
- Yahoo Finance fallback now reliably switches over when IBKR is
  unreachable, rather than blocking on the failed primary.

### Documentation
- New "Agent Skill Install Pattern" section in `CLAUDE.md`.
- Reworked Agent Skill section in `README.md` to cover the canonical
  `~/.agents/skills/optix/` layout, dev/release modes, `OPTIX_HOME`
  override, and `--uninstall --purge`.

[Unreleased]: https://github.com/IS908/optix/compare/v0.15.3...HEAD
[0.15.3]: https://github.com/IS908/optix/compare/v0.15.2...v0.15.3
[0.15.2]: https://github.com/IS908/optix/compare/v0.15.1...v0.15.2
[0.15.1]: https://github.com/IS908/optix/compare/v0.15.0...v0.15.1
[0.15.0]: https://github.com/IS908/optix/compare/v0.14.30...v0.15.0
[0.14.30]: https://github.com/IS908/optix/compare/v0.14.29...v0.14.30
[0.14.29]: https://github.com/IS908/optix/compare/v0.14.28...v0.14.29
[0.14.28]: https://github.com/IS908/optix/compare/v0.14.27...v0.14.28
[0.14.27]: https://github.com/IS908/optix/compare/v0.14.26...v0.14.27
[0.14.26]: https://github.com/IS908/optix/compare/v0.14.25...v0.14.26
[0.14.25]: https://github.com/IS908/optix/compare/v0.14.24...v0.14.25
[0.14.24]: https://github.com/IS908/optix/compare/v0.14.23...v0.14.24
[0.14.23]: https://github.com/IS908/optix/compare/v0.14.22...v0.14.23
[0.14.22]: https://github.com/IS908/optix/compare/v0.14.21...v0.14.22
[0.14.21]: https://github.com/IS908/optix/compare/v0.14.20...v0.14.21
[0.14.20]: https://github.com/IS908/optix/compare/v0.14.19...v0.14.20
[0.14.19]: https://github.com/IS908/optix/compare/v0.14.18...v0.14.19
[0.14.18]: https://github.com/IS908/optix/compare/v0.14.17...v0.14.18
[0.14.17]: https://github.com/IS908/optix/compare/v0.14.16...v0.14.17
[0.14.16]: https://github.com/IS908/optix/compare/v0.14.15...v0.14.16
[0.14.15]: https://github.com/IS908/optix/compare/v0.14.14...v0.14.15
[0.14.14]: https://github.com/IS908/optix/compare/v0.14.13...v0.14.14
[0.14.13]: https://github.com/IS908/optix/compare/v0.14.12...v0.14.13
[0.14.12]: https://github.com/IS908/optix/compare/v0.14.11...v0.14.12
[0.14.11]: https://github.com/IS908/optix/compare/v0.14.10...v0.14.11
[0.14.10]: https://github.com/IS908/optix/compare/v0.14.9...v0.14.10
[0.14.9]: https://github.com/IS908/optix/compare/v0.14.8...v0.14.9
[0.14.8]: https://github.com/IS908/optix/compare/v0.14.7...v0.14.8
[0.14.7]: https://github.com/IS908/optix/compare/v0.14.6...v0.14.7
[0.14.6]: https://github.com/IS908/optix/compare/v0.14.5...v0.14.6
[0.14.5]: https://github.com/IS908/optix/compare/v0.14.4...v0.14.5
[0.14.4]: https://github.com/IS908/optix/compare/v0.14.3...v0.14.4
[0.14.3]: https://github.com/IS908/optix/compare/v0.14.2...v0.14.3
[0.14.2]: https://github.com/IS908/optix/compare/v0.14.1...v0.14.2
[0.14.1]: https://github.com/IS908/optix/compare/v0.14.0...v0.14.1
[0.14.0]: https://github.com/IS908/optix/compare/v0.13.0...v0.14.0
[0.13.0]: https://github.com/IS908/optix/compare/v0.12.1...v0.13.0
[0.12.1]: https://github.com/IS908/optix/compare/v0.12.0...v0.12.1
[0.12.0]: https://github.com/IS908/optix/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/IS908/optix/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/IS908/optix/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/IS908/optix/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/IS908/optix/compare/v0.7.25...v0.8.0
[0.7.25]: https://github.com/IS908/optix/compare/v0.7.24...v0.7.25
[0.7.24]: https://github.com/IS908/optix/compare/v0.7.23...v0.7.24
[0.7.23]: https://github.com/IS908/optix/compare/v0.7.22...v0.7.23
[0.7.22]: https://github.com/IS908/optix/compare/v0.7.21...v0.7.22
[0.7.21]: https://github.com/IS908/optix/compare/v0.7.20...v0.7.21
[0.7.20]: https://github.com/IS908/optix/compare/v0.7.19...v0.7.20
[0.7.19]: https://github.com/IS908/optix/compare/v0.7.18...v0.7.19
[0.7.18]: https://github.com/IS908/optix/compare/v0.7.17...v0.7.18
[0.7.17]: https://github.com/IS908/optix/compare/v0.7.16...v0.7.17
[0.7.16]: https://github.com/IS908/optix/compare/v0.7.15...v0.7.16
[0.7.15]: https://github.com/IS908/optix/compare/v0.7.14...v0.7.15
[0.7.14]: https://github.com/IS908/optix/compare/v0.7.13...v0.7.14
[0.7.13]: https://github.com/IS908/optix/compare/v0.7.12...v0.7.13
[0.7.12]: https://github.com/IS908/optix/compare/v0.7.11...v0.7.12
[0.7.11]: https://github.com/IS908/optix/compare/v0.7.10...v0.7.11
[0.7.10]: https://github.com/IS908/optix/compare/v0.7.9...v0.7.10
[0.7.9]: https://github.com/IS908/optix/compare/v0.7.8...v0.7.9
[0.7.8]: https://github.com/IS908/optix/compare/v0.7.7...v0.7.8
[0.7.7]: https://github.com/IS908/optix/compare/v0.7.6...v0.7.7
[0.7.6]: https://github.com/IS908/optix/compare/v0.7.5...v0.7.6
[0.7.5]: https://github.com/IS908/optix/compare/v0.7.4...v0.7.5
[0.7.4]: https://github.com/IS908/optix/compare/v0.7.3...v0.7.4
[0.7.3]: https://github.com/IS908/optix/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/IS908/optix/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/IS908/optix/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/IS908/optix/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/IS908/optix/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/IS908/optix/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/IS908/optix/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/IS908/optix/compare/v0.4.5...v0.5.0
[0.4.5]: https://github.com/IS908/optix/compare/v0.4.4...v0.4.5
[0.4.4]: https://github.com/IS908/optix/compare/v0.4.3...v0.4.4
[0.4.3]: https://github.com/IS908/optix/compare/v0.4.2...v0.4.3
[0.4.2]: https://github.com/IS908/optix/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/IS908/optix/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/IS908/optix/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/IS908/optix/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/IS908/optix/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/IS908/optix/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/IS908/optix/releases/tag/v0.1.0
