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
- `skills/commands/optix/SKILL.md`: corrected the wrapper invocation path
  back to `bin/optix.sh`. A concurrent edit during the v0.4.0 work
  rewrote every `bash bin/optix.sh ...` example to bare `bash optix.sh`,
  but the installed skill layout places the thin wrapper at
  `<skill_dir>/bin/optix.sh` (per `install.sh`'s own layout doc). Agents
  following the v0.4.0 descriptor hit `bash: optix.sh: No such file or
  directory`.

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

[Unreleased]: https://github.com/IS908/optix/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/IS908/optix/releases/tag/v0.2.0
[0.1.1]: https://github.com/IS908/optix/releases/tag/v0.1.1
[0.1.0]: https://github.com/IS908/optix/releases/tag/v0.1.0
