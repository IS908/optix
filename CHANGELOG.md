# Changelog

All notable changes to optix are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) and
correspond to git tags (`vMAJOR.MINOR.PATCH`).

When a release is cut, the `[Unreleased]` section below is renamed to the new
version with its release date, and a fresh empty `[Unreleased]` block is added
above it.

## [Unreleased]

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

[Unreleased]: https://github.com/IS908/optix/compare/v0.6.0...HEAD
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
