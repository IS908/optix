#!/usr/bin/env bash
#
# install.sh — install the optix skill for one or more agents.
#
# Layout (lark-* pattern):
#   ~/.agents/skills/optix/                          # canonical install (one copy)
#   ├── SKILL.md                                     # exact copy from repo
#   ├── bin/optix.sh                                 # thin wrapper (skill-wrapper.sh)
#   └── optix-repo  -> /path/to/IS908/optix          # symlink to project checkout
#
#   ~/.claude/skills/optix    -> ../../.agents/skills/optix    # per-agent symlink
#   ~/.openclaw/skills/optix  -> ../../.agents/skills/optix
#   ~/.hermes/skills/optix    -> ../../.agents/skills/optix
#
# This means SKILL.md is written ONCE, and is identical for all agents.
# Bumping the skill content is `git pull && install.sh --agent <name>` away.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)" && pwd)"

# Canonical location shared by all agents (matches the lark-* convention).
CANONICAL_DIR="$HOME/.agents/skills/optix"

AGENT=""
UNINSTALL=false
PURGE=false

usage() {
    cat <<EOF
Usage: install.sh [--agent <list>] [--uninstall] [--purge]

Options:
  --agent <list>   Comma-separated list of agents to install for.
                   Supported: claude, openclaw, hermes, generic
                   If omitted, auto-detects which agents are configured.
  --uninstall      Remove the per-agent symlinks (keeps canonical install).
  --purge          With --uninstall: also remove the canonical install at
                   ~/.agents/skills/optix and the slash command in this repo.

Examples:
  install.sh --agent claude
  install.sh --agent claude,hermes
  install.sh --uninstall --agent claude
  install.sh --uninstall --purge
EOF
}

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
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
        --purge)
            PURGE=true
            shift
            ;;
        -h|--help)
            usage; exit 0
            ;;
        *)
            usage; exit 1
            ;;
    esac
done

# ---------------------------------------------------------------------------
# Per-agent skill directory
# ---------------------------------------------------------------------------
agent_skill_dir() {
    case "$1" in
        claude)   echo "$HOME/.claude/skills/optix" ;;
        openclaw) echo "$HOME/.openclaw/skills/optix" ;;
        hermes)   echo "$HOME/.hermes/skills/optix" ;;
        generic)  echo "" ;;  # generic = no install, just print instructions
        *)        echo "" ;;
    esac
}

# ---------------------------------------------------------------------------
# Detect which agents are configured on this host.
# Returns a space-separated list via stdout.
# ---------------------------------------------------------------------------
detect_agents() {
    local found=()
    [[ -d "$HOME/.claude" ]]      && found+=("claude")
    [[ -d "$HOME/.openclaw" ]]    && found+=("openclaw")
    [[ -d "$HOME/.hermes" ]]      && found+=("hermes")
    if [[ ${#found[@]} -eq 0 ]]; then
        echo "generic"
    else
        echo "${found[@]}"
    fi
}

# ---------------------------------------------------------------------------
# Install the canonical skill at ~/.agents/skills/optix/. Idempotent.
# ---------------------------------------------------------------------------
install_canonical() {
    echo "Installing canonical skill at $CANONICAL_DIR"
    mkdir -p "$CANONICAL_DIR/bin"

    # 1) SKILL.md — copied verbatim from the repo, no path substitution.
    cp "$SCRIPT_DIR/SKILL.md" "$CANONICAL_DIR/SKILL.md"
    echo "  ✓ SKILL.md"

    # 2) bin/optix.sh — thin wrapper that discovers the optix project.
    cp "$SCRIPT_DIR/skill-wrapper.sh" "$CANONICAL_DIR/bin/optix.sh"
    chmod +x "$CANONICAL_DIR/bin/optix.sh"
    echo "  ✓ bin/optix.sh"

    # 3) optix-repo symlink — points to this checkout. The wrapper resolves
    #    this at runtime, so moving/reinstalling updates all agents at once.
    ln -snf "$PROJECT_ROOT" "$CANONICAL_DIR/optix-repo"
    echo "  ✓ optix-repo -> $PROJECT_ROOT"
}

# ---------------------------------------------------------------------------
# Create the per-agent symlink: ~/.<agent>/skills/optix -> canonical.
# Uses a relative target (../../.agents/skills/optix) when both live under
# $HOME, so it survives $HOME being a different mount on another machine.
# ---------------------------------------------------------------------------
link_agent() {
    local agent="$1"
    local agent_dir
    agent_dir="$(agent_skill_dir "$agent")"

    if [[ -z "$agent_dir" ]]; then
        return 0
    fi

    mkdir -p "$(dirname "$agent_dir")"

    # If a non-symlink directory already exists (legacy install), back it up.
    if [[ -d "$agent_dir" && ! -L "$agent_dir" ]]; then
        local backup="${agent_dir}.bak.$(date +%Y%m%d-%H%M%S)"
        echo "  ⚠️  $agent_dir is not a symlink; moving to $backup"
        mv "$agent_dir" "$backup"
    fi

    # Compute the relative path: same depth as ~/.<agent>/skills, target is ~/.agents/skills.
    # Both live under $HOME, so a 2-up relative is portable across machines.
    ln -snf "../../.agents/skills/optix" "$agent_dir"
    echo "  ✓ Linked $agent: $agent_dir -> ../../.agents/skills/optix"
}

# ---------------------------------------------------------------------------
# Per-agent post-install hooks (e.g. /optix slash command, openclaw safeBins).
# ---------------------------------------------------------------------------
post_install_agent() {
    local agent="$1"
    case "$agent" in
        claude)
            # Project-local /optix slash command for explicit invocation.
            mkdir -p "$PROJECT_ROOT/.claude/commands"
            cat > "$PROJECT_ROOT/.claude/commands/optix.md" <<'CMD_EOF'
# Optix — Stock & Options Analysis

Run optix CLI commands via the bundled wrapper. The skill is installed at
`~/.agents/skills/optix/` and symlinked into Claude's skill directory.

## Usage

```bash
bash ~/.agents/skills/optix/bin/optix.sh <command> [args]
```

## Commands
- `dashboard` — Watchlist overview
- `analyze <SYMBOL>` — Deep analysis (Python server auto-started)
- `quote <SYMBOL>` — Real-time quote (IBKR or yfinance fallback)
- `watch list|add <SYMBOL>|remove <SYMBOL>` — Manage watchlist

## Notes
- IBKR TWS/Gateway is **optional** — falls back to Yahoo Finance
- Python gRPC server auto-managed (port 50053)
- Override the optix project location via `OPTIX_HOME` environment variable
CMD_EOF
            echo "  ✓ Project-local slash command: .claude/commands/optix.md"
            ;;
        openclaw)
            # Register the wrapper in openclaw's safeBins allowlist so it can
            # be executed without per-call confirmation.
            local OPENCLAW_CONFIG="$HOME/.openclaw/openclaw.json"
            if [[ -f "$OPENCLAW_CONFIG" ]]; then
                local ABS_SCRIPT="$CANONICAL_DIR/bin/optix.sh"
                python3 - "$OPENCLAW_CONFIG" "$ABS_SCRIPT" <<'PY_EOF' || \
                    echo "  ⚠️  Failed to update openclaw.json safeBins; add manually." >&2
import json, sys
cfg_path, script = sys.argv[1], sys.argv[2]
with open(cfg_path) as f:
    cfg = json.load(f)
safe_bins = cfg.setdefault('tools', {}).setdefault('exec', {}).setdefault('safeBins', [])
profiles = cfg['tools']['exec'].setdefault('safeBinProfiles', {})
if script not in safe_bins:
    safe_bins.append(script)
    profiles[script.lower()] = {}
    with open(cfg_path, 'w') as f:
        json.dump(cfg, f, indent=2)
    print(f"  ✓ Registered {script} in openclaw.json safeBins")
else:
    print(f"  ✓ {script} already in safeBins")
PY_EOF
            else
                echo "  ⚠️  $OPENCLAW_CONFIG not found — skipping safeBins registration"
            fi
            ;;
    esac
}

# ---------------------------------------------------------------------------
# Uninstall: remove the per-agent symlink. With --purge, also remove canonical.
# ---------------------------------------------------------------------------
uninstall_agent() {
    local agent="$1"
    local agent_dir
    agent_dir="$(agent_skill_dir "$agent")"

    case "$agent" in
        claude)
            rm -f "$PROJECT_ROOT/.claude/commands/optix.md"
            echo "  ✓ Removed .claude/commands/optix.md"
            ;;
        openclaw)
            local OPENCLAW_CONFIG="$HOME/.openclaw/openclaw.json"
            if [[ -f "$OPENCLAW_CONFIG" ]]; then
                python3 - "$OPENCLAW_CONFIG" "$CANONICAL_DIR/bin/optix.sh" <<'PY_EOF' || true
import json, sys
cfg_path, script = sys.argv[1], sys.argv[2]
with open(cfg_path) as f:
    cfg = json.load(f)
safe_bins = cfg.get('tools', {}).get('exec', {}).get('safeBins', [])
profiles = cfg.get('tools', {}).get('exec', {}).get('safeBinProfiles', {})
if script in safe_bins:
    safe_bins.remove(script)
    profiles.pop(script.lower(), None)
    with open(cfg_path, 'w') as f:
        json.dump(cfg, f, indent=2)
    print(f"  ✓ Removed {script} from openclaw.json safeBins")
PY_EOF
            fi
            ;;
    esac

    if [[ -n "$agent_dir" && (-L "$agent_dir" || -e "$agent_dir") ]]; then
        rm -rf "$agent_dir"
        echo "  ✓ Removed $agent_dir"
    fi
}

# ---------------------------------------------------------------------------
# Resolve target list
# ---------------------------------------------------------------------------
INSTALL_TARGETS=()
if [[ -n "$AGENT" ]]; then
    IFS=',' read -ra INSTALL_TARGETS <<< "$AGENT"
else
    # shellcheck disable=SC2207
    INSTALL_TARGETS=( $(detect_agents) )
    echo "Auto-detected agents: ${INSTALL_TARGETS[*]}"
fi

# ---------------------------------------------------------------------------
# UNINSTALL
# ---------------------------------------------------------------------------
if [[ "$UNINSTALL" == true ]]; then
    for target in "${INSTALL_TARGETS[@]}"; do
        echo ""
        echo "--- Uninstalling for: $target ---"
        uninstall_agent "$target"
    done

    if [[ "$PURGE" == true ]]; then
        echo ""
        echo "--- Purging canonical install ---"
        rm -rf "$CANONICAL_DIR"
        echo "  ✓ Removed $CANONICAL_DIR"
    fi
    echo ""
    echo "Done."
    exit 0
fi

# ---------------------------------------------------------------------------
# INSTALL
# ---------------------------------------------------------------------------
echo "Checking prerequisites..."
if ! command -v go &>/dev/null; then
    echo "ERROR: Go compiler not found. Install Go first." >&2
    exit 1
fi
if ! command -v python3 &>/dev/null; then
    echo "ERROR: Python 3 not found. Install Python 3.11+ first." >&2
    exit 1
fi
if ! python3 -c "import sys; sys.exit(0 if sys.version_info >= (3, 11) else 1)" 2>/dev/null; then
    PY_VER="$(python3 -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')" 2>/dev/null || echo "unknown")"
    echo "ERROR: Python >= 3.11 is required (found: $PY_VER)" >&2
    exit 1
fi
if [[ ! -d "$PROJECT_ROOT/python/.venv" ]]; then
    echo "WARNING: Python venv not found at $PROJECT_ROOT/python/.venv" >&2
    echo "  Run: python3 -m venv python/.venv && python/.venv/bin/pip install -e python/" >&2
fi

echo ""
echo "Building optix CLI..."
make -C "$PROJECT_ROOT" build

echo ""
install_canonical

for target in "${INSTALL_TARGETS[@]}"; do
    echo ""
    echo "--- Installing for: $target ---"
    case "$target" in
        generic)
            cat <<EOF
  Manual installation: copy or symlink $CANONICAL_DIR into your agent's
  skill directory. The skill expects:
    - cwd = skill directory when commands are invoked
    - bin/optix.sh resolvable from cwd
    - $CANONICAL_DIR/optix-repo symlink valid OR \$OPTIX_HOME set
EOF
            ;;
        *)
            link_agent "$target"
            post_install_agent "$target"
            ;;
    esac
done

# ---------------------------------------------------------------------------
# Verify
# ---------------------------------------------------------------------------
echo ""
echo "Verifying installation..."
if "$CANONICAL_DIR/bin/optix.sh" watch list 2>/dev/null; then
    echo "✓ Wrapper works."
else
    echo "  Wrapper invoked successfully (watchlist may be empty)."
fi

echo ""
echo "Done. Skill installed for: ${INSTALL_TARGETS[*]}"
echo "  Canonical: $CANONICAL_DIR"
echo "  Project:   $PROJECT_ROOT"
