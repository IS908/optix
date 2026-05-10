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

[Unreleased]: https://github.com/IS908/optix/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/IS908/optix/releases/tag/v0.1.0
