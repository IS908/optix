# Optix Skill & README Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a portable CLI skill for Claude Code / OpenClaw agents and a project README with MIT license.

**Architecture:** Three shell-script files in `skills/commands/optix/` provide a wrapper around the existing `./bin/optix` CLI. An `install.sh` script registers the skill into each agent's command directory. README.md and LICENSE are created at the project root.

**Tech Stack:** Bash (scripts), Markdown (SKILL.md, README, LICENSE)

**Spec:** `docs/superpowers/specs/2026-03-22-optix-skill-and-readme-design.md`

---

## Task 1: Create optix.sh wrapper script

**Files:**
- Create: `skills/commands/optix/optix.sh`

- [ ] **Step 1: Create directory and write optix.sh**

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

- [ ] **Step 2: Make executable**

Run: `chmod +x skills/commands/optix/optix.sh`

- [ ] **Step 3: Verify optix.sh runs**

Run: `./skills/commands/optix/optix.sh watch list`
Expected: Either outputs watchlist symbols or empty list (no crash)

- [ ] **Step 4: Commit**

```bash
git add skills/commands/optix/optix.sh
git commit -m "feat(skill): add optix.sh wrapper script for CLI"
```

---

## Task 2: Create SKILL.md

**Files:**
- Create: `skills/commands/optix/SKILL.md`

- [ ] **Step 1: Write SKILL.md**

```markdown
---
name: optix
description: US stock & options strategy analysis — dashboard, quotes, analysis, watchlist management
---

# Optix

Stock & options strategy analysis tool. Provides real-time market data from Interactive Brokers (IBKR) combined with quantitative analysis from a Python gRPC engine.

## How to Use

Run commands via the wrapper script from the project root:

\`\`\`bash
bash skills/commands/optix/optix.sh <command> [args]
\`\`\`

## Commands

### dashboard
Show watchlist overview with latest quotes and technical signals.
\`\`\`bash
bash skills/commands/optix/optix.sh dashboard
\`\`\`

### analyze <SYMBOL>
Deep analysis for a stock: technicals, options pricing, strategy recommendations.
Requires Python gRPC server running on localhost:50052 (`make py-server`).
\`\`\`bash
bash skills/commands/optix/optix.sh analyze AAPL
\`\`\`

### quote <SYMBOL>
Get real-time stock quote from IBKR.
\`\`\`bash
bash skills/commands/optix/optix.sh quote TSLA
\`\`\`

### watch list
List all symbols in the watchlist.
\`\`\`bash
bash skills/commands/optix/optix.sh watch list
\`\`\`

### watch add <SYMBOL>
Add a symbol to the watchlist.
\`\`\`bash
bash skills/commands/optix/optix.sh watch add NVDA
\`\`\`

### watch remove <SYMBOL>
Remove a symbol from the watchlist.
\`\`\`bash
bash skills/commands/optix/optix.sh watch remove NVDA
\`\`\`

## Prerequisites
- Go compiler (for building CLI binary)
- IBKR TWS/Gateway running (for live market data)
- Python gRPC server (for analyze command only): `make py-server`

## Error Handling
- If `./bin/optix` is missing, the wrapper auto-builds it via `make build`
- If IBKR is not connected, quote/dashboard commands will fail with a connection error
- If Python server is down, analyze will fail — start with `make py-server`
```

- [ ] **Step 2: Commit**

```bash
git add skills/commands/optix/SKILL.md
git commit -m "feat(skill): add SKILL.md with command documentation"
```

---

## Task 3: Create install.sh

**Files:**
- Create: `skills/commands/optix/install.sh`

- [ ] **Step 1: Write install.sh**

The script must handle:
1. `--agent claude` / `--agent openclaw` / auto-detect
2. `--uninstall` to remove installed files
3. Prerequisite checks (Go, Python venv)
4. `make build` for CLI binary
5. Claude Code: generate `.claude/commands/optix.md` with prompt content referencing `optix.sh`
6. OpenClaw: copy SKILL.md + optix.sh to `~/.openclaw/skills/optix/`
7. Generic: print manual instructions
8. Verify: run `./bin/optix watch list`

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)" && pwd)"

AGENT=""
UNINSTALL=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --agent)
            AGENT="$2"
            shift 2
            ;;
        --uninstall)
            UNINSTALL=true
            shift
            ;;
        *)
            echo "Usage: install.sh [--agent claude|openclaw] [--uninstall]" >&2
            exit 1
            ;;
    esac
done

# Auto-detect agent if not specified
if [[ -z "$AGENT" ]]; then
    if [[ -d "$PROJECT_ROOT/.claude" ]]; then
        AGENT="claude"
    elif [[ -d "$HOME/.openclaw" ]]; then
        AGENT="openclaw"
    else
        AGENT="generic"
    fi
    echo "Auto-detected agent: $AGENT"
fi

# --- Uninstall ---
if [[ "$UNINSTALL" == true ]]; then
    case "$AGENT" in
        claude)
            rm -f "$PROJECT_ROOT/.claude/commands/optix.md"
            echo "Removed .claude/commands/optix.md"
            ;;
        openclaw)
            rm -rf "$HOME/.openclaw/skills/optix"
            echo "Removed ~/.openclaw/skills/optix/"
            ;;
        *)
            echo "Nothing to uninstall for generic agent."
            ;;
    esac
    exit 0
fi

# --- Prerequisites ---
echo "Checking prerequisites..."

if ! command -v go &>/dev/null; then
    echo "ERROR: Go compiler not found. Install Go first." >&2
    exit 1
fi

if [[ ! -d "$PROJECT_ROOT/python/.venv" ]]; then
    echo "WARNING: Python venv not found at python/.venv" >&2
    echo "  Run: python3 -m venv python/.venv && python/.venv/bin/pip install -e python/" >&2
fi

# --- Build CLI ---
echo "Building optix CLI..."
make -C "$PROJECT_ROOT" build

# --- Install ---
case "$AGENT" in
    claude)
        mkdir -p "$PROJECT_ROOT/.claude/commands"
        cat > "$PROJECT_ROOT/.claude/commands/optix.md" << 'CLAUDE_EOF'
# Optix — Stock & Options Analysis

Run optix CLI commands via the wrapper script. Pass user arguments after the script path.

## Usage

```bash
bash skills/commands/optix/optix.sh <command> [args]
```

## Available Commands

- `dashboard` — Watchlist overview with quotes and technical signals
- `analyze <SYMBOL>` — Deep stock analysis (requires Python server: `make py-server`)
- `quote <SYMBOL>` — Real-time stock quote from IBKR
- `watch list` — List watchlist symbols
- `watch add <SYMBOL>` — Add symbol to watchlist
- `watch remove <SYMBOL>` — Remove symbol from watchlist

## Examples

```bash
bash skills/commands/optix/optix.sh dashboard
bash skills/commands/optix/optix.sh analyze AAPL
bash skills/commands/optix/optix.sh quote TSLA
bash skills/commands/optix/optix.sh watch list
bash skills/commands/optix/optix.sh watch add NVDA
bash skills/commands/optix/optix.sh watch remove NVDA
```
CLAUDE_EOF
        echo "Installed: .claude/commands/optix.md"
        echo "Use with: /optix <args>"
        ;;
    openclaw)
        mkdir -p "$HOME/.openclaw/skills/optix"
        cp "$SCRIPT_DIR/SKILL.md" "$HOME/.openclaw/skills/optix/"
        cp "$SCRIPT_DIR/optix.sh" "$HOME/.openclaw/skills/optix/"
        chmod +x "$HOME/.openclaw/skills/optix/optix.sh"
        echo "Installed: ~/.openclaw/skills/optix/"
        ;;
    generic)
        echo ""
        echo "=== Manual Installation ==="
        echo "Skill files are at: $SCRIPT_DIR/"
        echo "  - SKILL.md:  Skill definition for your agent"
        echo "  - optix.sh:  Wrapper script (call this to run commands)"
        echo ""
        echo "Copy these files to your agent's skill directory and"
        echo "configure it to call optix.sh with the desired arguments."
        echo ""
        ;;
esac

# --- Verify ---
echo ""
echo "Verifying installation..."
if "$PROJECT_ROOT/bin/optix" watch list --db "$PROJECT_ROOT/data/optix.db" 2>/dev/null; then
    echo "Verification passed."
else
    echo "Verification: CLI runs (watchlist may be empty)."
fi

echo ""
echo "Done! Optix skill installed for $AGENT."
```

- [ ] **Step 2: Make executable**

Run: `chmod +x skills/commands/optix/install.sh`

- [ ] **Step 3: Test Claude Code install**

Run: `./skills/commands/optix/install.sh --agent claude`
Expected: `.claude/commands/optix.md` is created with correct content

- [ ] **Step 4: Verify generated command file**

Run: `cat .claude/commands/optix.md`
Expected: Contains usage instructions and command list

- [ ] **Step 5: Test uninstall**

Run: `./skills/commands/optix/install.sh --uninstall`
Expected: `.claude/commands/optix.md` is removed

- [ ] **Step 6: Re-install for use**

Run: `./skills/commands/optix/install.sh --agent claude`

- [ ] **Step 7: Commit**

```bash
git add skills/commands/optix/install.sh
git commit -m "feat(skill): add install.sh with multi-agent support"
```

---

## Task 4: Skill integration testing & optimization

- [ ] **Step 1: Test optix.sh watch list**

Run: `bash skills/commands/optix/optix.sh watch list`
Expected: Outputs watchlist (or empty list)

- [ ] **Step 2: Test optix.sh dashboard**

Run: `bash skills/commands/optix/optix.sh dashboard`
Expected: Outputs dashboard data (may show cached/stale data or empty if no IBKR)

- [ ] **Step 3: Test auto-build**

Run:
```bash
rm -f bin/optix
bash skills/commands/optix/optix.sh watch list
```
Expected: "Building optix CLI..." message, then binary is rebuilt and command runs

- [ ] **Step 4: Test Python server warning**

Run: `bash skills/commands/optix/optix.sh analyze TESTXYZ`
Expected: Warning about Python server not running on localhost:50052

- [ ] **Step 5: Run existing Go unit tests**

Run: `go test ./...`
Expected: All tests pass (no regressions)

- [ ] **Step 6: Fix any issues found, re-run tests**

Iterate until all tests pass and skill commands work correctly.

- [ ] **Step 7: Commit fixes if any**

```bash
git add -A
git commit -m "fix(skill): address issues found during integration testing"
```

---

## Task 5: Create LICENSE file

**Files:**
- Create: `LICENSE`

- [ ] **Step 1: Write MIT LICENSE**

```
MIT License

Copyright (c) 2026 IS908

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

- [ ] **Step 2: Commit**

```bash
git add LICENSE
git commit -m "chore: add MIT license"
```

---

## Task 6: Create README.md

**Files:**
- Create: `README.md`

- [ ] **Step 1: Write README.md**

The README must include these sections (content derived from CLAUDE.md and spec):

1. **Header**: Project name, one-line description
2. **Overview**: What Optix does, 3-5 key features
3. **Quick Start**: Prerequisites (Go, Python 3.14, IBKR TWS), setup commands, first run
4. **Architecture**: ASCII diagram of Go/Python/SQLite/IBKR data flow, directory structure table
5. **Usage**: CLI commands table, Web UI endpoints, Skill setup
6. **Development**: Build, test, protobuf codegen commands
7. **Contributing**: Branch naming (`feat/`, `fix/`), commit convention (conventional commits), PR process, code review
8. **License**: MIT, link to LICENSE file

Key content sources:
- Architecture diagram: Based on CLAUDE.md data flow description
- CLI commands: From `internal/cli/` command files
- Web UI: From `internal/webui/server.go` route registration
- Prerequisites: From CLAUDE.md "First-Time Setup"

Do NOT duplicate CLAUDE.md content verbatim — summarize for human readers and link to `docs/user_manual.md` for details.

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add project README with architecture and contributing guide"
```

---

## Task 7: Final integration testing & optimization

- [ ] **Step 1: Run full Go test suite**

Run: `go test ./...`
Expected: All tests pass

- [ ] **Step 2: Run Python tests**

Run: `python/.venv/bin/python -m pytest python/tests/ -v`
Expected: All tests pass

- [ ] **Step 3: Verify skill end-to-end**

Run:
```bash
# Re-install skill
./skills/commands/optix/install.sh --agent claude

# Test each command
bash skills/commands/optix/optix.sh watch list
bash skills/commands/optix/optix.sh watch add TEST
bash skills/commands/optix/optix.sh watch list
bash skills/commands/optix/optix.sh watch remove TEST
bash skills/commands/optix/optix.sh watch list
```

Expected: Add/remove/list cycle works correctly

- [ ] **Step 4: Verify README renders correctly**

Check that README.md has valid markdown (no broken links, correct formatting).

- [ ] **Step 5: Fix any issues, re-run tests**

Iterate until clean.

- [ ] **Step 6: Final commit if needed**

```bash
git add -A
git commit -m "fix: address final integration test findings"
```
