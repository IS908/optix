# Optix

US stock & options strategy analysis tool — identify sell-side opportunities for upcoming expirations using real-time IBKR data and quantitative analysis.

> 中文版本: [README_CN.md](README_CN.md)

## Overview

Optix combines Interactive Brokers market data with a Python-powered analysis engine to help options sellers find opportunities:

- **Real-time quotes & option chains** via IBKR TWS/Gateway
- **Technical analysis** — SMA, EMA, RSI, MACD, Bollinger Bands, ATR
- **Options pricing** — Black-Scholes, Greeks, implied volatility, max pain
- **Strategy recommendations** — Covered calls, cash-secured puts, credit spreads, iron condors
- **Account holdings & trade journal** — positions snapshot with P&L, plus a persistent execution log with FIFO round-trip matching and retrospective stats (works around IBKR's 7-day history limit)
- **Market Intel** — phase-aware premarket, postclose, event-day, and shock dashboards with explicit source/basis/warnings
- **Web dashboard** with auto-refresh and data freshness tracking

> **Read-only with respect to IBKR.** Optix never places, modifies, or cancels orders. All trading operations must be performed by the user directly in TWS or Gateway.

## Quick Start

### Install from a release (recommended for end users)

Pick the latest release at <https://github.com/IS908/optix/releases> and download the tarball matching your OS/arch. The tarball contains a prebuilt binary, the Python engine source, the skill descriptor, and an `install.sh`.

```bash
VERSION=v0.14.11
OS=$(uname -s | tr '[:upper:]' '[:lower:]')        # darwin | linux
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')

curl -fL "https://github.com/IS908/optix/releases/download/${VERSION}/optix-skill-${VERSION}-${OS}-${ARCH}.tar.gz" \
  | tar -xz
cd "optix-skill-${VERSION}-${OS}-${ARCH}"

# Optional: verify checksum
curl -fsSL "https://github.com/IS908/optix/releases/download/${VERSION}/SHA256SUMS" -o SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing

# Install (auto-detects Claude / OpenClaw / Hermes; pass --agent <list> to be explicit)
./install.sh --agent claude
```

`install.sh` lays out `~/.agents/skills/optix/` (canonical bundle) and creates per-agent symlinks at `~/.<agent>/skills/optix`. The bundle includes `.runtime/` with the prebuilt binary and a freshly-created Python venv on your machine — see [Agent Skill → Layout](#layout) below.

After install, the binary is reachable via:

```bash
~/.agents/skills/optix/.runtime/bin/optix dashboard
~/.agents/skills/optix/.runtime/bin/optix analyze AAPL
~/.agents/skills/optix/.runtime/bin/optix quote TSLA
```

…or via the agent (just ask Claude "查一下 AAPL 股价" / "analyze TSLA").

To uninstall later (you don't need to keep the original tarball):

```bash
~/.agents/skills/optix/install.sh --uninstall --purge
```

### Build from source (developers)

```bash
# Prerequisites: Go 1.26+, Python 3.11+ (3.14 recommended), IBKR Gateway or TWS

git clone https://github.com/IS908/optix.git
cd optix

# Python dependencies
python3 -m venv python/.venv
python/.venv/bin/pip install -e 'python/[dev]'

# Build Go binaries
make build
```

### Run from source

```bash
# Terminal 1: Start Python analysis engine
make py-server

# Terminal 2: Start web UI (http://127.0.0.1:8080)
./bin/optix-server

# Or use CLI directly
./bin/optix dashboard
./bin/optix analyze AAPL
./bin/optix quote TSLA
```

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                     User Interface                   │
│         Web UI (:8080)  │  CLI (./bin/optix)         │
└────────────┬────────────┴──────────┬─────────────────┘
             │                       │
┌────────────▼───────────────────────▼─────────────────┐
│                   Go Backend                          │
│  broker/ibkr  │  webui  │  cli  │  datastore/sqlite  │
└───────┬───────┴─────────┴───────┴────────┬───────────┘
        │                                  │
        │ IBKR API                         │ gRPC (:50052)
        │                                  │
┌───────▼───────┐              ┌───────────▼───────────┐
│  IBKR TWS /   │              │   Python Engine        │
│  IB Gateway   │              │  technical / options /  │
│  (:4001)      │              │  strategy / sentiment   │
└───────────────┘              └───────────────────────┘
```

### Directory Structure

```
optix/
├── cmd/                    # Entry points
│   ├── optix-cli/          # Full CLI binary
│   └── optix-server/       # Web server binary
├── internal/
│   ├── broker/ibkr/        # IBKR integration
│   ├── analysis/           # gRPC client to Python engine
│   ├── cli/                # Cobra command definitions
│   ├── datastore/sqlite/   # SQLite persistence & caching
│   ├── journal/            # FIFO round-trip matching (pure function)
│   ├── portfolio/          # Account-level concentration/Greeks/stress views
│   ├── watchlist/          # Watchlist persistence manager
│   ├── scanjournal/        # Sell-put scan journal: register/reconcile/stats
│   ├── marketdata/         # Multi-asset pulse, yfinance helpers, source/basis labels
│   ├── intel/              # Market Intel phase clock, API handlers, judgment journal
│   ├── intraday/           # Intraday movers/sector-heatmap cards (IBKR-preferred)
│   ├── intelshared/        # Shared Market Intel helpers (NY clock, symbol normalization)
│   ├── premarket/          # M4 premarket cards
│   ├── postclose/          # M5 postclose cards
│   ├── eventintel/         # M6 event-day cards
│   ├── shockintel/         # M7 shock cards
│   ├── webui/              # HTTP server, templates, handlers
│   ├── scheduler/          # Background async refresh
│   └── server/             # gRPC server for market data
├── web/                    # Embedded Market Intel SPA served at /intel/
├── python/src/optix_engine/
│   ├── grpc_server/        # gRPC service implementation
│   ├── options/            # Black-Scholes, Greeks, IV
│   ├── technical/          # Indicators (SMA, RSI, MACD...)
│   └── strategy/           # Strategy recommendation logic
├── proto/optix/            # Protobuf definitions
├── skills/commands/optix/  # Claude Code / agent skill
└── docs/                   # User manual & design specs
```

## Usage

### CLI Commands

| Command | Description |
|---------|-------------|
| `./bin/optix dashboard` | Watchlist overview with quotes, technicals, recommendations |
| `./bin/optix analyze <SYMBOL> [--format json]` | Deep analysis: technicals + options + strategies |
| `./bin/optix analyze <SYMBOL> --with-oi [--expiry YYYY-MM-DD] [--format json]` | Same, plus per-contract Open Interest for Max Pain (needs OPRA subscription); `--expiry` picks a specific option expiration (default: nearest) |
| `./bin/optix analyze --watchlist [--format json]` | Deep analysis for every watchlist symbol; JSON mode returns one object with a per-symbol `results` array |
| `./bin/optix chain <SYMBOL> [--expiry YYYY-MM-DD]` | Option chain table via IBKR or delayed Yahoo Finance fallback |
| `./bin/optix option-quote <SYMBOL> --expiry YYYY-MM-DD --right C\|P --strike <N> [--format json]` | Single option contract real-time quote via IBKR (bid/ask/mid/mark/last/OI/IV/Greeks); IBKR-only, for final-candidate validation after chain screening |
| `./bin/optix max-pain <SYMBOL> [--expiry] [--source ibkr\|yfinance\|auto]` | Standalone Max Pain query for one option expiration |
| `./bin/optix quote <SYMBOL>` | Real-time stock quote |
| `./bin/optix positions [--type stk\|opt]` | IBKR account holdings with mark-to-market P&L (requires IBKR; option marks need OPRA) |
| `./bin/optix portfolio concentration [--net-liq-usd]` | Account-level concentration: per-name/per-sector weights vs NLV, threshold flags |
| `./bin/optix portfolio greeks` | Aggregated position-level Δ/Γ/Vega/Θ across all IBKR holdings |
| `./bin/optix portfolio stress [--net-liq-usd]` | Scenario P&L stress test using the Greeks snapshot |
| `./bin/optix trades [--symbol] [--side] [--since]` | IBKR execution history (last 7 days; `--since` older than 7d is clamped) |
| `./bin/optix journal status` | Trade journal sync state and size — **offline-safe**, no IBKR required |
| `./bin/optix journal sync` | Pull recent executions from IBKR into the local journal (idempotent) |
| `./bin/optix journal list [--symbol] [--since] [--until]` | List persisted executions (auto-syncs; `--no-sync` to skip) |
| `./bin/optix journal trips [--status open\|closed\|expired]` | FIFO-matched round trips with realized P&L |
| `./bin/optix journal review [--since] [--until]` | Retrospective summary: win rate, total P&L, by-symbol breakdown |
| `./bin/optix scan-journal register\|reconcile\|stats` | Sell-put scan journal: register candidates, settle expiries, banded hit-rate |
| `./bin/optix intel status` | Market Intel clock state and resolved view |
| `./bin/optix intel read\|narrative\|judge\|reconcile` | Judgment journal workflow for intraday hypotheses and reconciliation |
| `./bin/optix premarket [--format json]` | M4 premarket bundle: overnight chain, gap-fill stats, movers, sentiment |
| `./bin/optix postclose [--format json]` | M5 postclose bundle: earnings, timeline, read-across, after-hours movers |
| `./bin/optix event [--format json]` | M6 event-day bundle: rates path, FOMC diff, event patterns, sensitivity |
| `./bin/optix shock [--format json]` | M7 shock bundle: regime trigger, fingerprint, analogs, liquidity |
| `./bin/optix watch list` | List watchlist symbols |
| `./bin/optix watch add <SYMBOL>` | Add symbol to watchlist |
| `./bin/optix watch remove <SYMBOL>` | Remove symbol from watchlist |
| `./bin/optix pulse [--view premarket\|intraday\|postclose\|event\|shock]` | Multi-asset market snapshot (indices/futures/yields/vol/FX, no IBKR required; exposes per-asset `source` + `basis_note`) |
| `./bin/optix server` | Start web UI server |

### Web UI

Start with `./bin/optix-server` (default: `http://127.0.0.1:8080`).

| Route | Description |
|-------|-------------|
| `/dashboard` | Watchlist overview with auto-refresh |
| `/analyze/{symbol}` | Per-symbol deep analysis |
| `/watchlist` | Manage watchlist (add/remove) |
| `/journal` | Trade journal — Trades / Round Trips / Review tabs |
| `/intel/` | Embedded Market Intel SPA with auto phase routing and manual premarket/intraday/postclose/event/shock views |
| `/help` | Field reference documentation |
| `/api/dashboard` | JSON API for dashboard data |
| `/api/analyze/{symbol}` | JSON API for analysis data |
| `/api/freshness` | JSON: data freshness timestamps for all watchlist symbols |
| `/api/quotes` | JSON: lightweight ticker-zone quotes for all watchlist symbols (10s TTL cache) |
| `/api/quote/{symbol}` | JSON: lightweight single-symbol quote (10s TTL cache) |
| `/api/journal` | JSON: filtered executions list |
| `/api/journal/trips` | JSON: FIFO-matched round trips |
| `/api/journal/review` | JSON: aggregate stats |
| `POST /api/journal/sync` | Trigger sync from IBKR (502 + `ibkr_ok:false` on broker failure) |
| `/api/intel/state` | JSON: Market Intel phase, resolved view, and override reason |
| `/api/intel/pulse` | JSON: multi-asset pulse — same DTO schema as `optix pulse --format json`; auto-inferred view differs (HTTP auto-promotes to event/shock on FOMC/CPI days and shock regimes, the CLI does not) |
| `/api/intel/journal` | JSON: read-only judgment journal snapshot for the SPA narrative panel |
| `/api/intel/intraday/{movers,sector-heatmap}` | JSON: intraday movers and sector heatmap (IBKR-preferred) |
| `/api/intel/premarket/{overnight,gaps,movers,sentiment}` | JSON: M4 premarket cards |
| `/api/intel/postclose/{earnings,timeline,read-across,movers}` | JSON: M5 postclose cards |
| `/api/intel/event/{rates,diff,patterns,sensitivity}` | JSON: M6 event-day cards |
| `/api/intel/shock/{regime,fingerprint,analogs,liquidity}` | JSON: M7 shock cards |

Append `?refresh=true` to any page to fetch fresh data from IBKR instead of cache.

The web UI runs a background trade-journal sync ticker (`--journal-sync-interval=6h` by default; `0` disables) so users who keep `optix-server` running stay within IBKR's 7-day history window without manual sync.

### Agent Skill

Install the optix skill so AI agents (Claude Code, OpenClaw, Hermes) can run quote / analyze / watchlist commands on your behalf:

```bash
# Auto-detect which agents are configured on this machine
./skills/commands/optix/install.sh

# Or target specific agents
./skills/commands/optix/install.sh --agent claude
./skills/commands/optix/install.sh --agent claude,openclaw,hermes
```

The skill auto-triggers when you ask the agent things like "查一下 AAPL 股价" or "分析 TSLA"; explicit `/optix <command>` is also available in Claude Code.

<a id="layout"></a>
#### Layout

```
~/.agents/skills/optix/         ← canonical bundle (one copy, all agents share)
├── SKILL.md                     ← descriptor
├── bin/optix.sh                 ← entry wrapper
├── install.sh                   ← kept for later --uninstall --purge
└── .runtime                     ← symlink (dev) OR real dir (release)
    ├── bin/optix
    ├── python/.venv/
    ├── data/optix.db
    └── skills/commands/optix/optix.sh

~/.<agent>/skills/optix → ../../.agents/skills/optix
```

Two install modes are auto-detected by `install.sh`:
- **release** — running from an extracted release tarball: `.runtime` is a real directory with the bundled binary + a Python venv created on your machine
- **dev** — running from a source-tree checkout (`.git` + `Makefile` present): `.runtime` is a symlink to your repo, so `make build` edits take effect immediately

Override the runtime location with `export OPTIX_HOME=/path/to/optix` (useful when you have multiple checkouts or share dotfiles across machines).

#### Uninstall

```bash
./skills/commands/optix/install.sh --uninstall --agent claude   # remove per-agent symlink
./skills/commands/optix/install.sh --uninstall --purge          # also remove canonical bundle
```

### Scheduled Scans

`scripts/lark_nasdaq100_sell_put_scan.py` — Nasdaq-100 sell-put income scan built as a Lark (飞书) cron entry. yfinance screens the full index (7–24 DTE puts, ~5–18% OTM, OI ≥ 50, bid ≥ 0.20, spread ≤ 35%); the top candidates are then verified one contract at a time through `optix option-quote` (IBKR) and rendered as a dual-source Markdown table on stdout. Designed for **two** Asia/Shanghai cron entries (21:50 and 22:50) — the script self-gates to 09:45–10:10 ET on NYSE trading days, so exactly one entry fires regardless of US DST. Constituents come from the Nasdaq official API with slickcharts and a built-in list as fallbacks; a >20% fetch-failure circuit breaker marks the run invalid rather than reporting a plausible-looking empty result.

```bash
# Manual run (bypasses the time window; skips IBKR with --no-ibkr)
python scripts/lark_nasdaq100_sell_put_scan.py --dry-run --ibkr-top 3
```

Each live run also closes a falsifiable scan → outcome loop: the day's Top-10 candidates are registered into the scan journal (`optix scan-journal register`), and every previously-registered candidate whose expiry has passed is settled (`reconcile`) with the result appended as a「复盘」section in the Lark message; journal failures degrade to an in-message warning line rather than blocking the scan. Journal writes run automatically on live (non-`--dry-run`) invocations — pass `--no-journal` to skip them entirely, or `--with-journal` to force them on a `--dry-run` run too.

The scanner is portfolio-aware: candidates overlapping current IBKR holdings
(stock, or an existing short put on the same name/expiry) are annotated and
de-ranked in the table without affecting top-N membership or journal
comparability; disable with `--no-portfolio`.

## Development

### Build

```bash
make build          # Build both CLI and server binaries
make build-cli      # Build CLI only (bin/optix)
make build-server   # Build server only (bin/optix-server)
```

### Test

```bash
make test               # Go + Python unit tests
make test-integration   # Integration tests (auto-starts Python server)
```

### Protobuf

```bash
make proto    # Regenerate Go/Python code from .proto files
```

### IBKR Configuration

| Setting | Default | Flag |
|---------|---------|------|
| Host | `127.0.0.1` | `--ib-host` |
| Port | `gateway` (4001) | `--ib-port` |

`--ib-port` accepts aliases: `gateway` (4001), `tws` (7496), or a numeric port (e.g., `7497` for paper TWS, `4002` for paper Gateway).

## Contributing

### Branch Naming

- `feat/<description>` — New features
- `fix/<description>` — Bug fixes
- `chore/<description>` — Maintenance, dependencies
- `docs/<description>` — Documentation changes

### Commit Convention

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(webui): add data freshness panel
fix(broker): handle IBKR connection timeout
chore: update Go dependencies
docs: add contributing guide
```

### Pull Request Process

1. Create a feature branch from `master`
2. Make changes with clear, focused commits
3. Ensure `make test` passes
4. Open a PR with a description of changes and testing done
5. Address review feedback

### Code Style

- **Go**: Standard `gofmt` formatting
- **Python**: Check with `ruff` (`make lint-python` after installing `python/[dev]`)
- **Protobuf**: Follow [Buf style guide](https://buf.build/docs/best-practices/style-guide/)

## Releases

Tagged releases are published at <https://github.com/IS908/optix/releases> with prebuilt tarballs for `darwin-{arm64,amd64}` and `linux-{amd64,arm64}`. See [`CHANGELOG.md`](CHANGELOG.md) for the full version history.

**Users**: see [Quick Start → Install from a release](#install-from-a-release-recommended-for-end-users) for the install command.

**Maintainers** cutting a release:

```bash
git tag v1.2.3
git push origin v1.2.3
# .github/workflows/release.yml runs the {darwin,linux}×{amd64,arm64} matrix
# and publishes the release with all four tarballs + SHA256SUMS + CHANGELOG.md
```

## License

[MIT](LICENSE)
