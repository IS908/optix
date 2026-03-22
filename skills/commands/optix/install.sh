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

# ---------------------------------------------------------------------------
# Bundle: compile Go binary + create standalone Python venv in target dir
# ---------------------------------------------------------------------------
bundle_to() {
    local TARGET_DIR="$1"

    echo "  Bundling Go binary..."
    mkdir -p "$TARGET_DIR/bin"
    make -C "$PROJECT_ROOT" build
    cp "$PROJECT_ROOT/bin/optix" "$TARGET_DIR/bin/optix"

    echo "  Bundling Python engine..."
    mkdir -p "$TARGET_DIR/python"
    # Copy Python source package
    cp -r "$PROJECT_ROOT/python/src" "$TARGET_DIR/python/src"
    cp "$PROJECT_ROOT/python/pyproject.toml" "$TARGET_DIR/python/pyproject.toml"
    # Copy generated protobuf code
    if [[ -d "$PROJECT_ROOT/python/src/optix_engine/gen" ]]; then
        cp -r "$PROJECT_ROOT/python/src/optix_engine/gen" "$TARGET_DIR/python/src/optix_engine/gen"
    fi

    # Create a fresh venv and install the package
    echo "  Creating Python venv (this may take a moment)..."
    local PYTHON_BIN
    PYTHON_BIN="$(command -v python3.14 || command -v python3 || echo python3)"
    "$PYTHON_BIN" -m venv "$TARGET_DIR/python/.venv"
    "$TARGET_DIR/python/.venv/bin/pip" install --quiet -e "$TARGET_DIR/python/"

    # Create data directory
    mkdir -p "$TARGET_DIR/data"

    # Copy existing database if present (watchlist etc.)
    if [[ -f "$PROJECT_ROOT/data/optix.db" ]]; then
        cp "$PROJECT_ROOT/data/optix.db" "$TARGET_DIR/data/optix.db"
        echo "  Copied existing database (watchlist preserved)"
    fi

    echo "  ✓ Bundle complete: $TARGET_DIR"
}

# ---------------------------------------------------------------------------
# Generate standalone optix.sh that references SKILL_DIR, not PROJECT_ROOT
# ---------------------------------------------------------------------------
write_standalone_optix_sh() {
    local TARGET_DIR="$1"
    # optix.sh lives in bin/, so SKILL_ROOT is one level up
    cat > "$TARGET_DIR/bin/optix.sh" << 'WRAPPER_EOF'
#!/usr/bin/env bash
set -euo pipefail

# This script lives in <skill>/bin/; skill root is one level up
SKILL_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Skill uses a dedicated port to avoid conflict with local dev servers (50052)
ANALYSIS_PORT="${OPTIX_ANALYSIS_PORT:-50053}"
ANALYSIS_ADDR="localhost:${ANALYSIS_PORT}"

# --- Check IBKR TWS/Gateway for commands that need live data ---
IB_HOST="${OPTIX_IB_HOST:-127.0.0.1}"
IB_PORT="${OPTIX_IB_PORT:-7496}"

case "${1:-}" in
    quote|analyze|dashboard|chain)
        if ! nc -z "$IB_HOST" "$IB_PORT" 2>/dev/null; then
            echo "⚠️  IBKR TWS/Gateway is not running at ${IB_HOST}:${IB_PORT}" >&2
            echo "   Please start TWS or IB Gateway and enable API connections." >&2
            echo "   TWS: File → Global Configuration → API → Settings → Enable ActiveX and Socket Clients" >&2
            echo "   Ports: TWS live=7496, paper=7497 | Gateway live=4001, paper=4002" >&2
            exit 1
        fi
        ;;
esac

# --- Determine if command needs Python gRPC server ---
PY_SERVER_PID=""
NEED_PY_SERVER=false
EXTRA_ARGS=()

case "${1:-}" in
    analyze|dashboard)
        NEED_PY_SERVER=true
        EXTRA_ARGS+=(--analysis-addr "$ANALYSIS_ADDR")
        ;;
esac

if [[ "$NEED_PY_SERVER" == true ]]; then
    if ! nc -z localhost "$ANALYSIS_PORT" 2>/dev/null; then
        echo "Starting Python analysis server on port ${ANALYSIS_PORT}..." >&2
        "$SKILL_ROOT/python/.venv/bin/python" -m optix_engine.grpc_server.server --addr="$ANALYSIS_ADDR" &>/dev/null &
        PY_SERVER_PID=$!
        for i in {1..60}; do
            if nc -z localhost "$ANALYSIS_PORT" 2>/dev/null; then
                echo "Python analysis server ready." >&2
                break
            fi
            sleep 0.5
        done
        if ! nc -z localhost "$ANALYSIS_PORT" 2>/dev/null; then
            echo "ERROR: Python analysis server failed to start within 30s" >&2
        fi
    fi
fi

# --- Cleanup on exit: stop Python server if we started it ---
cleanup() {
    if [[ -n "$PY_SERVER_PID" ]]; then
        kill "$PY_SERVER_PID" 2>/dev/null
        wait "$PY_SERVER_PID" 2>/dev/null
    fi
}
trap cleanup EXIT

"$SKILL_ROOT/bin/optix" --db "$SKILL_ROOT/data/optix.db" "$@" ${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"}
WRAPPER_EOF
    chmod +x "$TARGET_DIR/bin/optix.sh"
}

# ---------------------------------------------------------------------------
# Per-agent install
# ---------------------------------------------------------------------------
install_agent() {
    local agent="$1"
    echo ""
    echo "--- Installing for: $agent ---"
    case "$agent" in
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

Replace `<SYMBOL>` with the actual stock ticker (e.g., AAPL, TSLA, NVDA).

- `dashboard` — Watchlist overview with quotes and technical signals
- `analyze <SYMBOL>` — Deep stock analysis (Python server auto-started)
- `quote <SYMBOL>` — Real-time stock quote from IBKR
- `watch list` — List watchlist symbols
- `watch add <SYMBOL>` — Add symbol to watchlist
- `watch remove <SYMBOL>` — Remove symbol from watchlist

## Examples

```bash
bash skills/commands/optix/optix.sh dashboard
bash skills/commands/optix/optix.sh analyze <SYMBOL>
bash skills/commands/optix/optix.sh quote <SYMBOL>
bash skills/commands/optix/optix.sh watch list
bash skills/commands/optix/optix.sh watch add <SYMBOL>
bash skills/commands/optix/optix.sh watch remove <SYMBOL>
```
CLAUDE_EOF
            echo "  ✓ Installed: .claude/commands/optix.md"
            echo "  Use with: /optix <args>"
            ;;
        openclaw)
            local INSTALL_DIR="$HOME/.openclaw/skills/optix"
            local OPENCLAW_CONFIG="$HOME/.openclaw/openclaw.json"

            # 1. Bundle binary + Python + data into skill dir
            mkdir -p "$INSTALL_DIR"
            # Patch SKILL.md: replace source paths with installed bin/optix.sh
            sed 's|skills/commands/optix/optix\.sh|bin/optix.sh|g' \
                "$SCRIPT_DIR/SKILL.md" > "$INSTALL_DIR/SKILL.md"
            bundle_to "$INSTALL_DIR"
            write_standalone_optix_sh "$INSTALL_DIR"

            # 2. Register bin/optix.sh in tools.exec.safeBins
            if [[ -f "$OPENCLAW_CONFIG" ]]; then
                local ABS_SCRIPT="$INSTALL_DIR/bin/optix.sh"
                if python3 -c "
import json, sys
with open('$OPENCLAW_CONFIG', 'r') as f:
    cfg = json.load(f)
safe_bins = cfg.setdefault('tools', {}).setdefault('exec', {}).setdefault('safeBins', [])
profiles = cfg['tools']['exec'].setdefault('safeBinProfiles', {})
script = '$ABS_SCRIPT'
if script not in safe_bins:
    safe_bins.append(script)
    profiles[script.lower()] = {}
    with open('$OPENCLAW_CONFIG', 'w') as f:
        json.dump(cfg, f, indent=2)
    print(f'  Registered {script} in openclaw.json safeBins')
else:
    print(f'  {script} already registered in safeBins')
" 2>&1; then
                    :
                else
                    echo "  WARNING: Failed to register in openclaw.json. Add manually:" >&2
                    echo "    safeBins: [\"$ABS_SCRIPT\"]" >&2
                fi
            else
                echo "  WARNING: openclaw.json not found at $OPENCLAW_CONFIG" >&2
            fi

            echo "  ✓ Installed: $INSTALL_DIR"
            echo "  CLI wrapper:   $INSTALL_DIR/bin/optix.sh"
            echo "  Python server: $INSTALL_DIR/python/.venv/bin/python -m optix_engine.grpc_server.server"
            ;;
        generic)
            echo ""
            echo "  === Manual Installation ==="
            echo "  Skill files are at: $SCRIPT_DIR/"
            echo "    - SKILL.md:  Skill definition for your agent"
            echo "    - optix.sh:  Wrapper script (call this to run commands)"
            echo ""
            echo "  Copy these files to your agent's skill directory and"
            echo "  configure it to call optix.sh with the desired arguments."
            ;;
    esac
}

# ---------------------------------------------------------------------------
# Per-agent uninstall
# ---------------------------------------------------------------------------
uninstall_agent() {
    local agent="$1"
    echo ""
    echo "--- Uninstalling for: $agent ---"
    case "$agent" in
        claude)
            rm -f "$PROJECT_ROOT/.claude/commands/optix.md"
            echo "  ✓ Removed .claude/commands/optix.md"
            ;;
        openclaw)
            local OPENCLAW_CONFIG="$HOME/.openclaw/openclaw.json"
            if [[ -f "$OPENCLAW_CONFIG" ]]; then
                local ABS_SCRIPT="$HOME/.openclaw/skills/optix/bin/optix.sh"
                python3 -c "
import json
with open('$OPENCLAW_CONFIG', 'r') as f:
    cfg = json.load(f)
safe_bins = cfg.get('tools', {}).get('exec', {}).get('safeBins', [])
profiles = cfg.get('tools', {}).get('exec', {}).get('safeBinProfiles', {})
script = '$ABS_SCRIPT'
if script in safe_bins:
    safe_bins.remove(script)
    profiles.pop(script.lower(), None)
    with open('$OPENCLAW_CONFIG', 'w') as f:
        json.dump(cfg, f, indent=2)
    print(f'  Removed {script} from openclaw.json safeBins')
" 2>/dev/null || true
            fi
            rm -rf "$HOME/.openclaw/skills/optix"
            echo "  ✓ Removed ~/.openclaw/skills/optix/"
            ;;
        generic)
            echo "  Nothing to uninstall for generic agent."
            ;;
    esac
}

# ---------------------------------------------------------------------------
# Interactive agent selection if not specified
# ---------------------------------------------------------------------------
INSTALL_TARGETS=()

if [[ -n "$AGENT" ]]; then
    INSTALL_TARGETS=("$AGENT")
else
    AVAILABLE=()
    if command -v claude &>/dev/null || [[ -d "$PROJECT_ROOT/.claude" ]]; then
        AVAILABLE+=("claude")
    fi
    if [[ -d "$HOME/.openclaw" ]]; then
        AVAILABLE+=("openclaw")
    fi

    if [[ ${#AVAILABLE[@]} -eq 0 ]]; then
        INSTALL_TARGETS=("generic")
        echo "No known agent detected, using generic mode."
    elif [[ ${#AVAILABLE[@]} -eq 1 ]]; then
        INSTALL_TARGETS=("${AVAILABLE[0]}")
        echo "Detected agent: ${AVAILABLE[0]}"
    else
        echo "Multiple agents detected. Select target:"
        echo ""
        for i in "${!AVAILABLE[@]}"; do
            echo "  $((i+1))) ${AVAILABLE[$i]}"
        done
        echo "  A) All of the above"
        echo ""
        read -rp "Choose [1-${#AVAILABLE[@]}/A]: " CHOICE
        if [[ "$CHOICE" =~ ^[Aa]$ ]]; then
            INSTALL_TARGETS=("${AVAILABLE[@]}")
        elif [[ "$CHOICE" =~ ^[0-9]+$ ]] && (( CHOICE >= 1 && CHOICE <= ${#AVAILABLE[@]} )); then
            INSTALL_TARGETS=("${AVAILABLE[$((CHOICE-1))]}")
        else
            echo "Invalid choice." >&2
            exit 1
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Uninstall
# ---------------------------------------------------------------------------
if [[ "$UNINSTALL" == true ]]; then
    for target in "${INSTALL_TARGETS[@]}"; do
        uninstall_agent "$target"
    done
    exit 0
fi

# ---------------------------------------------------------------------------
# Prerequisites
# ---------------------------------------------------------------------------
echo "Checking prerequisites..."

if ! command -v go &>/dev/null; then
    echo "ERROR: Go compiler not found. Install Go first." >&2
    exit 1
fi

if [[ ! -d "$PROJECT_ROOT/python/.venv" ]]; then
    echo "WARNING: Python venv not found at python/.venv" >&2
    echo "  Run: python3 -m venv python/.venv && python/.venv/bin/pip install -e python/" >&2
fi

# ---------------------------------------------------------------------------
# Build & Install
# ---------------------------------------------------------------------------
echo "Building optix CLI..."
make -C "$PROJECT_ROOT" build

for target in "${INSTALL_TARGETS[@]}"; do
    install_agent "$target"
done

# ---------------------------------------------------------------------------
# Verify
# ---------------------------------------------------------------------------
echo ""
echo "Verifying installation..."

# For openclaw, verify using the bundled binary; for claude, use project binary
VERIFY_BIN="$PROJECT_ROOT/bin/optix"
VERIFY_DB="$PROJECT_ROOT/data/optix.db"
for target in "${INSTALL_TARGETS[@]}"; do
    if [[ "$target" == "openclaw" ]]; then
        VERIFY_BIN="$HOME/.openclaw/skills/optix/bin/optix"
        VERIFY_DB="$HOME/.openclaw/skills/optix/data/optix.db"
        break
    fi
done

if "$VERIFY_BIN" watch list --db "$VERIFY_DB" 2>/dev/null; then
    echo "Verification passed."
else
    echo "Verification: CLI runs (watchlist may be empty)."
fi

echo ""
echo "Done! Optix skill installed for: ${INSTALL_TARGETS[*]}"
