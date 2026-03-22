# Optix Skill & README Design Spec

**Date:** 2026-03-22
**Status:** Draft
**Scope:** Two deliverables — a Claude Code skill for optix CLI, and a project README.md

---

## 1. Optix CLI Skill

### 1.1 Directory Structure

```
skills/commands/optix/
├── SKILL.md       # Skill definition — tells the agent what operations are available
├── optix.sh       # Wrapper script — calls ./bin/optix with auto-build and error handling
└── install.sh     # Installation script — registers skill into agent environments
```

Source lives in `skills/commands/optix/` (version-controlled). The `install.sh` script copies/symlinks files into each agent's skill directory.

**How Claude Code discovers this skill:** `install.sh` creates `.claude/commands/optix.md` which is a markdown file that includes instructions for the agent to call `optix.sh`. Claude Code's slash command system picks up `.md` files in `.claude/commands/` — the filename `optix.md` becomes the `/optix` command.

**Note:** `./bin/optix` is the compiled binary from `cmd/optix-cli/main.go` (the full CLI with all subcommands), not from `cmd/optix-server`.

### 1.2 Supported Operations

| Invocation | CLI Mapping | Purpose |
|------------|-------------|---------|
| `/optix dashboard` | `./bin/optix dashboard` | Watchlist overview with key indicators |
| `/optix analyze <SYMBOL>` | `./bin/optix analyze <SYMBOL>` | Deep stock + options analysis |
| `/optix quote <SYMBOL>` | `./bin/optix quote <SYMBOL>` | Real-time stock quote |
| `/optix watch list` | `./bin/optix watch list` | List all watchlist symbols |
| `/optix watch add <SYMBOL>` | `./bin/optix watch add <SYMBOL>` | Add symbol to watchlist |
| `/optix watch remove <SYMBOL>` | `./bin/optix watch remove <SYMBOL>` | Remove symbol from watchlist |

### 1.3 optix.sh

Wrapper script responsibilities:

1. **Auto-build**: Check if `./bin/optix` exists; if not, run `make build`
2. **Default flags**: Pass `--db ./data/optix.db` (user-supplied `--db` in `$@` appears later and Cobra uses last-wins, so it overrides correctly)
3. **Argument passthrough**: Forward all arguments to `./bin/optix`
4. **Error handling**: Exit with CLI's exit code, stderr passed through
5. **Python server check**: For `analyze` command, check if Python gRPC server is reachable on `localhost:50052`; warn if not

```bash
#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(git -C "$(dirname "$0")" rev-parse --show-toplevel)" && pwd)"

# Auto-build if binary missing
if [[ ! -x "$PROJECT_ROOT/bin/optix" ]]; then
    echo "Building optix CLI..." >&2
    make -C "$PROJECT_ROOT" build >&2
fi

# Warn if Python server needed but not running
if [[ "${1:-}" == "analyze" ]]; then
    if ! command -v nc &>/dev/null || ! nc -z localhost 50052 2>/dev/null; then
        echo "Warning: Python analysis server may not be running on localhost:50052" >&2
        echo "Start it with: make -C '$PROJECT_ROOT' py-server" >&2
    fi
fi

exec "$PROJECT_ROOT/bin/optix" --db "$PROJECT_ROOT/data/optix.db" "$@"
```

### 1.4 SKILL.md

The SKILL.md provides the full prompt context for the agent. Skeleton:

```markdown
---
name: optix
description: US stock & options strategy analysis — dashboard, quotes, analysis, watchlist management
---

# Optix

Stock & options strategy analysis tool. Run commands via the wrapper script.

## How to Use

Run `bash skills/commands/optix/optix.sh <command> [args]` from the project root.

## Commands

### dashboard
Show watchlist overview with latest quotes and technical signals.
Example: `bash skills/commands/optix/optix.sh dashboard`

### analyze <SYMBOL>
Deep analysis for a stock: technicals, options pricing, strategy recommendations.
Requires Python gRPC server running on localhost:50052.
Example: `bash skills/commands/optix/optix.sh analyze AAPL`

### quote <SYMBOL>
Get real-time stock quote from IBKR.
Example: `bash skills/commands/optix/optix.sh quote TSLA`

### watch list
List all symbols in the watchlist.
Example: `bash skills/commands/optix/optix.sh watch list`

### watch add <SYMBOL>
Add a symbol to the watchlist.
Example: `bash skills/commands/optix/optix.sh watch add NVDA`

### watch remove <SYMBOL>
Remove a symbol from the watchlist.
Example: `bash skills/commands/optix/optix.sh watch remove NVDA`

## Prerequisites
- Go compiler (for building CLI)
- IBKR TWS/Gateway running (for live data)
- Python gRPC server (for analyze command): `make py-server`

## Error Handling
- If `./bin/optix` is missing, the wrapper auto-builds it
- If IBKR is not connected, quote/dashboard commands will fail with connection error
- If Python server is down, analyze will fail — start with `make py-server`
```

### 1.5 install.sh

Multi-agent installation script:

```bash
./skills/commands/optix/install.sh              # Auto-detect agent
./skills/commands/optix/install.sh --agent claude    # Claude Code
./skills/commands/optix/install.sh --agent openclaw  # OpenClaw
```

**Installation targets by agent:**

| Agent | Target | Method | Details |
|-------|--------|--------|---------|
| Claude Code | `<project>/.claude/commands/optix.md` | Generate command file | Creates a `.md` file that instructs Claude to call `optix.sh` |
| OpenClaw | `~/.openclaw/skills/optix/` | Copy files | Copies SKILL.md + optix.sh (aspirational — format TBD when OpenClaw stabilizes) |
| Generic | N/A | Print instructions | Outputs paths and manual setup steps |

**Claude Code install detail:** The script generates `.claude/commands/optix.md` with content that references the `optix.sh` wrapper path. This makes `/optix <args>` available as a slash command.

**Installation steps:**

1. Detect agent type (check for `.claude/` dir, `~/.openclaw/` dir, or `--agent` flag)
2. Verify prerequisites: Go compiler, Python venv with optix_engine
3. Run `make build` to compile CLI binary
4. For Claude Code: generate `.claude/commands/optix.md` pointing to `skills/commands/optix/optix.sh`
5. For OpenClaw: copy skill files to `~/.openclaw/skills/optix/`
6. Verify: run `./bin/optix watch list` to confirm CLI works
7. Print success message with usage examples

**Uninstall:** `install.sh --uninstall` removes the installed files from the agent directory.

---

## 2. README.md

### 2.1 Location

Project root: `README.md`

### 2.2 Structure

1. **Project Title & One-liner** — Name, brief description
2. **Overview** — What Optix does, key features (3-5 bullets)
3. **Quick Start** — Prerequisites, install steps, first run (both Web UI and CLI)
4. **Architecture** — ASCII diagram showing Go/Python/SQLite/IBKR layers, directory structure table
5. **Usage** — CLI command reference, Web UI endpoints, Skill usage
6. **Development** — Build, test, protobuf codegen, code style
7. **Contributing** — Branch naming, commit conventions, PR process, code review
8. **License** — MIT

### 2.3 Design Principles

- **Complements CLAUDE.md**: README targets human readers (new contributors, users). CLAUDE.md targets AI assistants. No content duplication.
- **Concise**: Link to `docs/user_manual.md` for deep dives rather than duplicating.
- **Architecture diagram**: ASCII text art, no external image dependencies.
- **Self-contained Quick Start**: Clone → install → run in under 5 minutes.

### 2.4 License

MIT License. A `LICENSE` file will be created at the project root. (User confirmed MIT choice.)

---

## 3. Testing & Iteration Plan

After each deliverable is implemented:

### 3.1 Skill testing (new tests)

- Run `install.sh --agent claude` and verify `.claude/commands/optix.md` is created correctly
- Run `optix.sh watch list` directly and confirm output
- Run `optix.sh dashboard` and confirm output
- Test auto-build: delete `./bin/optix`, run `optix.sh watch list`, verify it rebuilds then succeeds
- Test Python server warning: stop py-server, run `optix.sh analyze AAPL`, verify warning message
- Test error handling: run `optix.sh quote INVALID_SYMBOL_XYZ`
- Test uninstall: run `install.sh --uninstall`, verify `.claude/commands/optix.md` is removed

### 3.2 Existing test suite (regression)

- `make test` — Go + Python unit tests (verify no regressions)
- `make test-integration` — Full integration with Python gRPC server (if IBKR available)

### 3.3 Optimization loop

- Identify issues from test results
- Fix code
- Re-run tests
- Repeat until clean

---

## 4. Deliverable Order

1. Implement skill (`skills/commands/optix/` with SKILL.md, optix.sh, install.sh)
2. Test skill → optimize loop
3. Implement README.md + LICENSE
4. Test full integration → optimize loop
5. Final commit
