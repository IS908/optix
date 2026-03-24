# Optix

US stock & options strategy analysis tool — identify sell-side opportunities for upcoming expirations using real-time IBKR data and quantitative analysis.

## Overview

Optix combines Interactive Brokers market data with a Python-powered analysis engine to help options sellers find opportunities:

- **Real-time quotes & option chains** via IBKR TWS/Gateway
- **Technical analysis** — SMA, EMA, RSI, MACD, Bollinger Bands, ATR
- **Options pricing** — Black-Scholes, Greeks, implied volatility, max pain
- **Strategy recommendations** — Covered calls, cash-secured puts, credit spreads, iron condors
- **Web dashboard** with auto-refresh and data freshness tracking

## Quick Start

### Prerequisites

- **Go** 1.22+
- **Python** 3.11+ (3.14 recommended)
- **Interactive Brokers** TWS or IB Gateway running with API enabled

### Setup

```bash
# Clone
git clone https://github.com/IS908/optix.git
cd optix

# Python dependencies
python3 -m venv python/.venv
python/.venv/bin/pip install -e python/

# Build Go binaries
make build
```

### Run

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
│   ├── webui/              # HTTP server, templates, handlers
│   ├── scheduler/          # Background async refresh
│   └── server/             # gRPC server for market data
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
| `./bin/optix analyze <SYMBOL>` | Deep analysis: technicals + options + strategies |
| `./bin/optix quote <SYMBOL>` | Real-time stock quote |
| `./bin/optix watch list` | List watchlist symbols |
| `./bin/optix watch add <SYMBOL>` | Add symbol to watchlist |
| `./bin/optix watch remove <SYMBOL>` | Remove symbol from watchlist |
| `./bin/optix server` | Start web UI server |

### Web UI

Start with `./bin/optix-server` (default: `http://127.0.0.1:8080`).

| Route | Description |
|-------|-------------|
| `/dashboard` | Watchlist overview with auto-refresh |
| `/analyze/{symbol}` | Per-symbol deep analysis |
| `/watchlist` | Manage watchlist (add/remove) |
| `/help` | Field reference documentation |
| `/api/dashboard` | JSON API for dashboard data |
| `/api/analyze/{symbol}` | JSON API for analysis data |

Append `?refresh=true` to any page to fetch fresh data from IBKR instead of cache.

### Agent Skill

Install the optix skill for your AI coding agent:

```bash
# Claude Code
./skills/commands/optix/install.sh --agent claude

# OpenClaw
./skills/commands/optix/install.sh --agent openclaw

# Then use: /optix dashboard, /optix analyze AAPL, etc.
```

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
- **Python**: Format with `ruff` (`python/.venv/bin/ruff check python/`)
- **Protobuf**: Follow [Buf style guide](https://buf.build/docs/best-practices/style-guide/)

## License

[MIT](LICENSE)
