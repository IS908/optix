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
python3.14 -m venv python/.venv
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

- Default connection: `127.0.0.1:7496` (live TWS)
- Override with flags: `--ib-host` and `--ib-port`
- Paper trading port: `7497`
- Gateway port: `4001` (live) or `4002` (paper)

## Code Architecture

### Go Structure (`internal/`)

**`broker/`**: Abstraction layer for market data sources
- `broker.go`: Interface definition (`Connect`, `GetQuote`, `GetHistoricalBars`, `GetOptionChain`)
- `ibkr/`: Interactive Brokers implementation using `github.com/scmhub/ibapi`

**`analysis/`**: gRPC client to Python engine
- `client.go`: Go wrapper for `AnalysisService` (PriceOption, GetMaxPain, AnalyzeStock, BatchQuickAnalysis)
- Integration tests require Python server running

**`datastore/sqlite/`**: SQLite persistence layer
- Caches stock quotes, option chains, analysis results, watchlists
- Schema in `migrations/001_initial.sql`
- Uses WAL mode for concurrent reads

**`webui/`**: Lightweight HTML/Go template server
- `server.go`: HTTP handlers for dashboard, watchlist, analyze pages
- `static/templates/`: HTML templates with Tailwind CSS
- `cache.go`: In-memory result caching with freshness tracking
- `live.go`: Live refresh logic (`?refresh=true` bypasses cache, hits IBKR+Python)

**`server/`**: gRPC server exposing market data to external clients
- `grpc.go`: Server setup
- `marketdata_svc.go`: Implements `MarketDataService` proto

**`cli/`**: Cobra command definitions
- `root.go`: Shared flags (`--db`, `--ib-host`, `--ib-port`)
- `server.go`: Web UI launch command
- `quote.go`, `chain.go`, `analyze.go`, `dashboard.go`, `watch.go`: CLI subcommands

### Python Structure (`python/src/optix_engine/`)

**`grpc_server/`**: gRPC service implementation
- `server.py`: Entry point, starts gRPC server on `localhost:50052`
- `analysis_servicer.py`: Implements `AnalysisService` RPCs

**`options/`**: Options pricing and Greeks
- Black-Scholes model, implied volatility calculations

**`technical/`**: Technical analysis indicators
- Custom implementations (SMA, EMA, RSI, MACD, Bollinger Bands, ATR) to avoid `pandas-ta` numba incompatibility with Python 3.14

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

### Two-Phase Refresh Model (Web UI)

The web UI serves data from SQLite cache by default. Use `?refresh=true` query param to trigger live IBKR + Python analysis:

1. **Cached mode** (default): Fast, stale-ok dashboard views
2. **Live refresh** (`?refresh=true`): Slow, fetches fresh data from IBKR, runs Python analysis, updates cache

Check `internal/webui/live.go` for refresh orchestration.

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

## Common Gotchas

- **Python module not found**: Run `python/.venv/bin/pip install -e python/` to install `optix-engine` package
- **Integration tests fail**: Ensure Python server is running on `localhost:50052`
- **IBKR connection errors**: Verify TWS/Gateway is running and API connections are enabled in settings
- **Protobuf changes not reflected**: Run `make proto` to regenerate Go/Python code
- **Port 7496 vs 7497**: 7496 = live TWS, 7497 = paper TWS (update `--ib-port` accordingly)
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
