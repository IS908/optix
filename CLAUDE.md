# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Optix is a **US stock & options strategy analysis tool** that helps identify sell-side opportunities for upcoming expirations. It combines real-time market data from Interactive Brokers (IBKR) with quantitative analysis powered by a Python gRPC engine.

**Architecture**: Hybrid Go + Python system
- **Go backend**: CLI tools, web server, IBKR integration, SQLite caching, gRPC orchestration
- **Python engine**: Technical analysis, options pricing (Black-Scholes), Greeks calculations, strategy recommendations

**Data flow**: IBKR TWS/Gateway → Go broker client → SQLite cache → Web UI or CLI → Python analysis engine (via gRPC) → Results

## Essential Commands

### First-Time Setup
```bash
# 1. Install Python dependencies (REQUIRED before first run)
python3 -m venv python/.venv  # requires Python 3.11+
python/.venv/bin/pip install -e python/

# 2. Build Go binaries
make build
```

### Running the Application

**Start the web UI** (recommended for most users):
```bash
# Start backend server (default: http://127.0.0.1:8080)
./bin/optix-server --web-addr 127.0.0.1:8080

# OR use make
make run-server
```

**Start Python analysis engine** (required for analysis features):
```bash
# Terminal 1: Python gRPC server
make py-server

# OR directly
python/.venv/bin/python -m optix_engine.grpc_server.server --addr=localhost:50052
```

**CLI usage examples**:
```bash
# Get stock quote
go run ./cmd/optix-cli quote AAPL

# View option chain
go run ./cmd/optix-cli chain AAPL --expiry 2024-03-15

# Run full analysis
go run ./cmd/optix-cli analyze TSLA

# Launch dashboard
go run ./cmd/optix-cli dashboard

# Manage watchlist
go run ./cmd/optix-cli watch add NVDA
go run ./cmd/optix-cli watch list
```

### Development

**Run tests**:
```bash
# Go unit tests
go test ./...

# Python unit tests
python/.venv/bin/python -m pytest python/tests/ -v

# Integration tests (starts Python server, runs Go tests, stops server)
make test-integration

# Single test
go test -v -run TestAnalysisClient ./internal/analysis/
```

**Regenerate protobuf code** (after editing `.proto` files):
```bash
make proto
# OR
./scripts/proto-gen.sh
```

**Clean build artifacts**:
```bash
make clean  # Removes bin/ and data/optix.db
```

### IBKR Configuration

- Default connection: `127.0.0.1:4001` (IB Gateway live)
- `--ib-port` accepts aliases: `gateway` (4001), `tws` (7496), or a numeric port
- Override host with `--ib-host`
- Paper trading ports: `7497` (TWS) or `4002` (Gateway)

## Code Architecture

### Go Structure (`internal/`)

**`broker/`**: Abstraction layer for market data sources
- `broker.go`: Interface definition (`Connect`, `GetQuote`, `GetHistoricalBars`, `GetOptionChain`)
- `ibkr/`: Interactive Brokers implementation using `github.com/scmhub/ibapi`

**`analysis/`**: gRPC client to Python engine
- `client.go`: Go wrapper for `AnalysisService` (PriceOption, GetMaxPain, AnalyzeStock, BatchQuickAnalysis)
- Integration tests require Python server running

**`marketdata/`**: Multi-asset market snapshot layer (indices/futures/yields/vol/FX)
- Business-ID `AssetRef` + `Source`/`Router` abstraction routes by `AssetClass` to pluggable sources (currently all via yfinance; zero IBKR dependency)
- `PulseService` with 60s in-memory TTL + SQLite `market_pulse_bars` two-tier cache (migration 005, 2-day rolling prune)
- `earnings.go`, `option_chain.go`, `raw_bars.go`: yfinance subprocess helpers for Market Intel views (earnings dates, Put/Call ratios, raw-ticker pre/post bars)

**`intel/`**: Market Intel scheduling plane (pure functions, zero IBKR/LLM)
- `phase.go`: four-phase market clock `PhaseAt`/`NextTransition`/`ViewFor` (premarket/intraday/postclose/closed)
- `calendar.go`: built-in NYSE 2026-2027 holiday/early-close calendar
- `checkpoints.go`/`journal.go`/`scorer.go`: judgment-journal domain — daily checkpoint schedule (剧本/首验/定调/对账), `IntelJournal` service (narrative + falsifiable-judgment writes via CLI, price capture, reconciliation, hit-rate), append-only over migration 006 tables
- `handlers.go`: `GET /api/intel/state` (phase + next transition), `/api/intel/pulse` (same DTO as `optix pulse --format json`, lifted here and shared by CLI + HTTP), `/api/intel/journal` (read-only journal snapshot for the SPA narrative panel), `/api/intel/premarket/{overnight,gaps,movers,sentiment}` (M4 premarket cards), `/api/intel/postclose/{earnings,timeline,read-across,movers}` (M5 postclose cards), `/api/intel/event/{rates,diff,patterns,sensitivity}` (M6 event cards), and `/api/intel/shock/{regime,fingerprint,analogs,liquidity}` (M7 shock cards)

**`premarket/`**: Market Intel premarket analysis plane (pure compute, zero IBKR/gRPC)
- `overnight.go`: descriptive overnight transmission chain (N225→TSMC→SX5E→ES)
- `gaps.go`: SPX implied open + historical gap-fill distribution (migration 007 cache, lazy TTL)
- `movers.go`: watchlist ∪ curated liquid tickers, premarket move and volume-ratio ranking
- `sentiment.go`: Put/Call + VIX3M/VIX term premium, degraded regime label
- `service.go`: `PremarketService` bundle and per-card failure isolation for CLI/HTTP

**`postclose/`**: Market Intel postclose analysis plane (pure compute, zero IBKR/gRPC)
- `earnings.go`: yfinance earnings dates filtered into EPS surprise reports (free/degraded consensus)
- `movers.go`: regular-session + after-hours + combined move extraction from raw 5m prepost bars
- `read_across.go`: same-sector read-across edges via the embedded sector map
- `timeline.go`: deterministic structured postclose event timeline
- `service.go`: `PostcloseService` bundle and per-card failure isolation for CLI/HTTP

**`eventintel/`**: Market Intel event-day analysis plane (pure compute, zero IBKR/gRPC)
- `rates.go`: US2Y/US10Y/DXY/GOLD/SPX/NDX/VIX pre-event baseline vs current quote repricing
- `diff.go`: deterministic FOMC statement sentence diff plus hawkish/dovish keyword scoring
- `patterns.go`: built-in FOMC/CPI calendar windows with T-1/T/T+1 historical pattern aggregation
- `sensitivity.go`: signed risk-on/rates-up/dollar-up sensitivity matrix from event-window returns
- `source.go`/`service.go`: yfinance adapter plus Fed.gov FOMC and BLS CPI official-source fetchers with local fixture fallback; degraded sources return warnings with non-nil DTO slices

**`shockintel/`**: Market Intel shock analysis plane (pure compute, IBKR-preferred by contract)
- `regime.go`: VIX-sigma plus cross-asset/liquidity confirmation into normal/watch/shock/critical trigger state
- `fingerprint.go`: supply/demand/liquidity/policy shock fingerprint scoring
- `analogs.go`: local historical shock-template similarity matching
- `liquidity.go`: ETF spread/depth liquidity state for SPY/QQQ/IWM/TLT/HYG/LQD
- `source.go`/`service.go`: broker-backed top-of-book quote adapter overlays IBKR/Yahoo ETF bid/ask data on yfinance macro sensors; IBKR SMART depth and broker/yfinance option-chain stress metrics populate where available, with explicit warnings when sources degrade

**`datastore/sqlite/`**: SQLite persistence layer
- Caches stock quotes, option chains, analysis results, watchlists
- Schema in `migrations/001_initial.sql`
- Uses WAL mode for concurrent reads

**`webui/`**: Lightweight HTML/Go template server
- `server.go`: HTTP handlers for dashboard, watchlist, analyze pages
- `static/templates/`: HTML templates with Tailwind CSS (dual-zone layout: ticker + analysis)
- `cache.go`: In-memory result caching with freshness tracking
- `live.go`: Live refresh logic (`?refresh=true` bypasses cache, hits IBKR+Python)
- `quotes.go`: Lightweight `/api/quotes` and `/api/quote/{symbol}` endpoints with TTL cache (10s)

**`server/`**: gRPC server exposing market data to external clients
- `grpc.go`: Server setup
- `marketdata_svc.go`: Implements `MarketDataService` proto

**`web/`** (repo root, not under `internal/`): Market Intel SPA (Vite + React + TS + Tailwind)
- `go:embed all:dist` into `optix-server`, served at `/intel/`; a committed placeholder `dist/index.html` keeps pure-Go builds working without node
- `make web` / `make build-full` build the SPA; build artifacts (`dist/assets`, `node_modules`) are gitignored

**`cli/`**: Cobra command definitions
- `root.go`: Shared flags (`--db`, `--ib-host`, `--ib-port`)
- `server.go`: Web UI launch command (also wires the `/api/intel/*` handlers + `/intel/` SPA)
- `quote.go`, `chain.go`, `analyze.go`, `dashboard.go`, `watch.go`, `positions.go`, `trades.go`, `journal.go`, `maxpain.go`, `portfolio.go`, `pulse.go`, `intel.go`, `premarket.go`, `postclose.go`, `event.go`, `shock.go`: CLI subcommands

### Python Structure (`python/src/optix_engine/`)

**`grpc_server/`**: gRPC service implementation
- `server.py`: Entry point, starts gRPC server on `localhost:50052`
- `analysis_servicer.py`: Implements `AnalysisService` RPCs

**`options/`**: Options pricing and Greeks
- Black-Scholes model, implied volatility calculations

**`technical/`**: Technical analysis indicators
- Custom implementations (SMA, EMA, RSI, MACD, Bollinger Bands, ATR) to avoid `pandas-ta` numba incompatibility with Python >= 3.13

**`strategy/`**: Strategy recommendation logic
- Evaluates covered calls, cash-secured puts, credit spreads based on technical signals, IV rank, support/resistance

**`sentiment/`**: Sentiment analysis (future expansion)

**`report/`**: Report generation utilities

### Protobuf Definitions (`proto/optix/`)

- **`marketdata/v1/`**: Shared types (OptionChain, Greeks, OHLCV bars)
- **`analysis/v1/`**: Analysis service contract (PriceOption, GetMaxPain, AnalyzeStock, RecommendStrategies)

Generated Go code: `gen/go/optix/{marketdata,analysis}/v1/`
Generated Python code: `python/src/optix_engine/gen/optix/{marketdata,analysis}/v1/`

## Important Patterns

### Three-Tier Refresh Model (Web UI)

The web UI uses a dual-zone layout (upper ticker zone + lower analysis zone) with three data tiers:

1. **Ticker zone** (`/api/quotes`, `/api/quote/{symbol}`): Lightweight broker-only quotes with 10s TTL cache. Polled every 30s by frontend JS. Response time: <1ms (cached) / ~4ms (cold).
2. **Cached analysis** (default): Serves dashboard and analyze pages from SQLite cache.
3. **Live refresh** (`?refresh=true`): Full broker + Python pipeline, updates cache.

During **extended hours** (pre-market/post-market), the ticker zone shows real-time prices, but strategy calculations use the previous close for reliability (adaptive strategy mode).

Check `internal/webui/quotes.go` for ticker data, `live.go` for full analysis orchestration.

### Integration Testing

Integration tests (`-tags=integration`) **require the Python gRPC server running**. The `make test-integration` target handles this automatically:

```go
// +build integration

func TestAnalysisClient(t *testing.T) {
    client, _ := analysis.NewClient("localhost:50052")
    // ...
}
```

Run manually:
```bash
# Terminal 1
make py-server

# Terminal 2
go test -tags=integration -v ./internal/analysis/
```

### Python Virtualenv Path

All Python commands **must** use `python/.venv/bin/python` or activate the venv. The Makefile hardcodes this via `PYTHON := python/.venv/bin/python`.

### Database Location

Default: `./data/optix.db` (relative to CWD). Override with `--db` flag. The SQLite store auto-creates the directory if missing.

### Agent Skill Install Pattern

`skills/commands/optix/install.sh` installs the skill to a single canonical location (`~/.agents/skills/optix/`) and creates per-agent symlinks at `~/.<agent>/skills/optix`. Auto-detects two modes:

- **dev mode** (source checkout, `.git` + `Makefile` present): `.runtime/` becomes a symlink to the source tree. `make build` edits take effect immediately.
- **release mode** (extracted tarball, future): `.runtime/` is a real directory with bundled binary + on-machine Python venv.

Source tree and `.runtime/` share the same internal layout (`bin/`, `python/`, `data/`, `skills/commands/optix/`), so the wrapper (`skill-wrapper.sh`) has no branching logic — runtime resolution is a single chain: `$OPTIX_HOME` → `<skill>/.runtime` → `command -v optix`.

When changing skill behavior, edit `skills/commands/optix/SKILL.md` (descriptor) and `optix.sh` (orchestration: Python server lifecycle, IBKR probe, port resolution). The skill wrapper itself (`skill-wrapper.sh`) should rarely change — it's just a thin redirector.

## Common Gotchas

- **Python module not found**: Run `python/.venv/bin/pip install -e python/` to install `optix-engine` package
- **Integration tests fail**: Ensure Python server is running on `localhost:50052`
- **IBKR connection errors**: Verify TWS/Gateway is running and API connections are enabled in settings
- **Protobuf changes not reflected**: Run `make proto` to regenerate Go/Python code
- **Port aliases**: `--ib-port=gateway` (4001, default), `--ib-port=tws` (7496), or use numeric port directly (e.g., `--ib-port=7497` for paper TWS)
- **Web UI shows stale data**: Use `?refresh=true` to bypass cache, or check `last_refreshed_at` timestamps in SQLite

## Dependencies

**Go** (go.mod):
- `github.com/scmhub/ibapi` – Interactive Brokers API client
- `github.com/spf13/cobra` – CLI framework
- `google.golang.org/grpc` – gRPC client
- `modernc.org/sqlite` – Pure-Go SQLite driver

**Python** (pyproject.toml):
- `grpcio` + `grpcio-tools` – gRPC server and protobuf codegen
- `numpy`, `scipy`, `pandas` – Numerical analysis
- `matplotlib` – Plotting (future feature)
- Dev: `pytest`, `ruff`, `mypy`, `py_vollib`

## Entry Points

- `cmd/optix-cli/main.go` – Full CLI with subcommands
- `cmd/optix-server/main.go` – Shortcut binary (defaults to `server` subcommand)
- `cmd/ibtest/main.go` – IBKR connection testing utility
- `python/src/optix_engine/grpc_server/server.py` – Python gRPC analysis engine
