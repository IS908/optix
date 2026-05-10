#!/usr/bin/env bash
#
# install.sh — install the optix skill for one or more agents.
#
# Modes (auto-detected):
#   • dev      — running from a source-tree checkout (.git present + Makefile)
#                .runtime/ is a symlink to the source tree, so edits + rebuild
#                take effect immediately. No file copying.
#
#   • release  — running from an extracted release tarball
#                .runtime/ is a real directory: copies bin/python/ into the
#                canonical skill dir and creates a Python venv on the user's
#                machine.
#
# Layout after install (lark-* convention):
#
#   ~/.agents/skills/optix/                  ← canonical, install once
#   ├── SKILL.md
#   ├── bin/optix.sh                          ← thin wrapper (skill-wrapper.sh)
#   └── .runtime                              ← symlink (dev) OR real dir (release)
#       ├── bin/optix
#       ├── python/{src,pyproject.toml,.venv}
#       ├── data/optix.db
#       └── skills/commands/optix/optix.sh    ← orchestration (Python server, IBKR probe)
#
#   ~/.<agent>/skills/optix → ../../.agents/skills/optix    ← per-agent symlink

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

CANONICAL_DIR="$HOME/.agents/skills/optix"
AGENT=""
UNINSTALL=false
PURGE=false

# MODE/PROJECT_ROOT are detected lazily — only required when installing.
# For --uninstall we operate purely on $CANONICAL_DIR and per-agent symlinks,
# so we don't need a source tree or tarball at all (which lets users uninstall
# from the install.sh copy stashed at $CANONICAL_DIR/install.sh long after the
# original tarball is gone).
MODE=""
PROJECT_ROOT=""

detect_mode() {
    if [[ -f "$SCRIPT_DIR/../../../Makefile" && -d "$SCRIPT_DIR/../../../.git" ]]; then
        MODE="dev"
        PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
    elif [[ -f "$SCRIPT_DIR/bin/optix" && -f "$SCRIPT_DIR/SKILL.md" ]]; then
        # Use -f (not -x): tar may strip the executable bit during extraction.
        # install.sh re-chmods +x when copying.
        MODE="release"
        PROJECT_ROOT="$SCRIPT_DIR"
    else
        echo "ERROR: cannot detect installation mode." >&2
        echo "  Expected either:" >&2
        echo "    • A source-tree checkout (.git + Makefile at \$SCRIPT_DIR/../../../)" >&2
        echo "    • A release tarball (bin/optix + SKILL.md alongside install.sh)" >&2
        exit 1
    fi
}

usage() {
    cat <<EOF
Usage: install.sh [--agent <list>] [--uninstall] [--purge]

Options:
  --agent <list>   Comma-separated agents (claude, openclaw, hermes, generic).
                   If omitted, auto-detects which agents are configured.
  --uninstall      Remove per-agent symlinks. Keeps canonical skill bundle.
  --purge          With --uninstall: also remove ~/.agents/skills/optix.

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
        --agent) AGENT="$2"; shift 2 ;;
        --uninstall) UNINSTALL=true; shift ;;
        --purge) PURGE=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) usage; exit 1 ;;
    esac
done

agent_skill_dir() {
    case "$1" in
        claude)   echo "$HOME/.claude/skills/optix" ;;
        openclaw) echo "$HOME/.openclaw/skills/optix" ;;
        hermes)   echo "$HOME/.hermes/skills/optix" ;;
        generic)  echo "" ;;
        *)        echo "" ;;
    esac
}

detect_agents() {
    local found=()
    [[ -d "$HOME/.claude" ]]   && found+=("claude")
    [[ -d "$HOME/.openclaw" ]] && found+=("openclaw")
    [[ -d "$HOME/.hermes" ]]   && found+=("hermes")
    if [[ ${#found[@]} -eq 0 ]]; then
        echo "generic"
    else
        echo "${found[@]}"
    fi
}

# ---------------------------------------------------------------------------
# Build the .runtime/ subdirectory.
#
# In dev mode it's a symlink to the source tree (zero-copy, edits take effect).
# In release mode it's a real directory with bin/, python/, data/ copied in,
# plus a Python venv created on the user's machine.
# ---------------------------------------------------------------------------
build_runtime() {
    local target="$CANONICAL_DIR/.runtime"

    case "$MODE" in
        dev)
            # Replace whatever's there with a symlink to the source tree.
            rm -rf "$target"
            ln -snf "$PROJECT_ROOT" "$target"
            echo "  ✓ .runtime → $PROJECT_ROOT (dev symlink)"

            # Verify the source tree has been built — we don't try to build it
            # for the user; that's their dev-loop responsibility.
            if [[ ! -x "$PROJECT_ROOT/bin/optix" ]]; then
                echo "  ⚠️  $PROJECT_ROOT/bin/optix not found — run 'make build' before invoking the skill" >&2
            fi
            if [[ ! -x "$PROJECT_ROOT/python/.venv/bin/python" ]]; then
                echo "  ⚠️  $PROJECT_ROOT/python/.venv not found — run 'python3 -m venv python/.venv && python/.venv/bin/pip install -e python/' before invoking the skill" >&2
            fi
            ;;

        release)
            # If a previous install left a symlink here, drop it.
            [[ -L "$target" ]] && rm -f "$target"
            mkdir -p "$target"

            echo "  Copying bin/optix..."
            mkdir -p "$target/bin"
            cp "$PROJECT_ROOT/bin/optix" "$target/bin/optix"
            chmod +x "$target/bin/optix"

            echo "  Copying Python engine source..."
            rm -rf "$target/python/src"
            mkdir -p "$target/python"
            cp -r "$PROJECT_ROOT/python/src" "$target/python/src"
            cp "$PROJECT_ROOT/python/pyproject.toml" "$target/python/pyproject.toml"

            echo "  Copying skill orchestration script..."
            mkdir -p "$target/skills/commands/optix"
            cp "$PROJECT_ROOT/skills/commands/optix/optix.sh" "$target/skills/commands/optix/optix.sh"
            chmod +x "$target/skills/commands/optix/optix.sh"

            echo "  Creating data directory..."
            mkdir -p "$target/data"

            create_release_venv "$target/python"
            ;;
    esac
}

# ---------------------------------------------------------------------------
# Build the Python venv inside the release bundle. Tries host Python first
# and only creates a real venv if dependencies are missing.
# ---------------------------------------------------------------------------
create_release_venv() {
    local PY_TARGET="$1"
    local PYTHON_BIN
    PYTHON_BIN="$(command -v python3.14 || command -v python3.13 || command -v python3.12 || command -v python3.11 || command -v python3 || echo python3)"

    if ! "$PYTHON_BIN" -c "import sys; sys.exit(0 if sys.version_info >= (3, 11) else 1)" 2>/dev/null; then
        echo "  ERROR: Python >= 3.11 required (found: $("$PYTHON_BIN" --version 2>&1 || echo 'none'))" >&2
        return 1
    fi
    echo "  Using Python: $("$PYTHON_BIN" --version 2>&1)"

    # Check whether the host Python already has every runtime dep we need.
    local HOST_OK=true
    "$PYTHON_BIN" -c "import numpy, scipy.stats, scipy.optimize, pandas, grpc, google.protobuf, yfinance" 2>/dev/null || HOST_OK=false

    if [[ "$HOST_OK" == true ]]; then
        echo "  Host Python has all deps — installing optix_engine into a thin shim venv"
        "$PYTHON_BIN" -m pip install --quiet --no-deps --user -e "$PY_TARGET" 2>/dev/null || \
            "$PYTHON_BIN" -m pip install --quiet --no-deps -e "$PY_TARGET" 2>/dev/null || true
        mkdir -p "$PY_TARGET/.venv/bin"
        ln -sf "$(command -v "$PYTHON_BIN")" "$PY_TARGET/.venv/bin/python"
    else
        echo "  Creating standalone venv (this may take a minute)..."
        "$PYTHON_BIN" -m venv "$PY_TARGET/.venv"
        "$PY_TARGET/.venv/bin/pip" install --quiet \
            "numpy>=1.26" "scipy>=1.12" "pandas>=2.2" "grpcio>=1.60" "protobuf>=4.25" "yfinance>=0.2"
        "$PY_TARGET/.venv/bin/pip" install --quiet --no-deps -e "$PY_TARGET"

        # Slim a bit
        "$PY_TARGET/.venv/bin/pip" uninstall --quiet -y pip setuptools 2>/dev/null || true
        find "$PY_TARGET/.venv" -type d -name "__pycache__" -exec rm -rf {} + 2>/dev/null || true
        find "$PY_TARGET/.venv" -name "*.pyc" -delete 2>/dev/null || true
    fi
}

# ---------------------------------------------------------------------------
# Install the canonical skill bundle at ~/.agents/skills/optix/.
# ---------------------------------------------------------------------------
install_canonical() {
    echo "Installing canonical skill at $CANONICAL_DIR (mode: $MODE)"
    mkdir -p "$CANONICAL_DIR/bin"

    cp "$SCRIPT_DIR/SKILL.md" "$CANONICAL_DIR/SKILL.md"
    echo "  ✓ SKILL.md"

    cp "$SCRIPT_DIR/skill-wrapper.sh" "$CANONICAL_DIR/bin/optix.sh"
    chmod +x "$CANONICAL_DIR/bin/optix.sh"
    echo "  ✓ bin/optix.sh (entry wrapper)"

    # Stash a copy of install.sh so users can uninstall later without
    # needing to keep the original tarball around. They can run:
    #   bash ~/.agents/skills/optix/install.sh --uninstall --purge
    cp "$SCRIPT_DIR/install.sh" "$CANONICAL_DIR/install.sh"
    chmod +x "$CANONICAL_DIR/install.sh"
    echo "  ✓ install.sh (kept for later uninstall)"

    build_runtime
}

# ---------------------------------------------------------------------------
# Per-agent symlink: ~/.<agent>/skills/optix -> ../../.agents/skills/optix
# Relative target keeps the link valid across $HOME mount differences.
# ---------------------------------------------------------------------------
link_agent() {
    local agent="$1"
    local agent_dir
    agent_dir="$(agent_skill_dir "$agent")"
    [[ -z "$agent_dir" ]] && return 0

    mkdir -p "$(dirname "$agent_dir")"

    # Back up legacy non-symlink installs.
    if [[ -d "$agent_dir" && ! -L "$agent_dir" ]]; then
        local backup="${agent_dir}.bak.$(date +%Y%m%d-%H%M%S)"
        echo "  ⚠️  $agent_dir is a directory, not a symlink; moving to $backup"
        mv "$agent_dir" "$backup"
    fi

    ln -snf "../../.agents/skills/optix" "$agent_dir"
    echo "  ✓ Linked $agent: $agent_dir → ../../.agents/skills/optix"
}

# ---------------------------------------------------------------------------
# Optional per-agent post-install hooks (e.g. openclaw safeBins registration).
# ---------------------------------------------------------------------------
post_install_agent() {
    local agent="$1"
    case "$agent" in
        openclaw)
            local cfg="$HOME/.openclaw/openclaw.json"
            local script="$CANONICAL_DIR/bin/optix.sh"
            if [[ -f "$cfg" ]]; then
                python3 - "$cfg" "$script" <<'PY' || \
                    echo "  ⚠️  Failed to register in openclaw safeBins; add manually." >&2
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
    print(f"  ✓ Registered {script} in openclaw safeBins")
else:
    print(f"  ✓ {script} already in safeBins")
PY
            fi
            ;;
    esac
}

# ---------------------------------------------------------------------------
# Uninstall: removes per-agent symlinks. With --purge, also removes canonical.
# ---------------------------------------------------------------------------
uninstall_agent() {
    local agent="$1"
    local agent_dir
    agent_dir="$(agent_skill_dir "$agent")"

    case "$agent" in
        openclaw)
            local cfg="$HOME/.openclaw/openclaw.json"
            if [[ -f "$cfg" ]]; then
                python3 - "$cfg" "$CANONICAL_DIR/bin/optix.sh" <<'PY' || true
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
    print(f"  ✓ Removed {script} from openclaw safeBins")
PY
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
detect_mode

echo "Mode: $MODE"
echo "Project root: $PROJECT_ROOT"
echo ""

# Dev-mode prereq: ensure source is buildable (we don't try to build for the user).
if [[ "$MODE" == "dev" ]]; then
    if [[ ! -x "$PROJECT_ROOT/bin/optix" ]]; then
        echo "Building optix CLI..."
        make -C "$PROJECT_ROOT" build
    fi
fi

install_canonical

for target in "${INSTALL_TARGETS[@]}"; do
    echo ""
    echo "--- Installing for: $target ---"
    case "$target" in
        generic)
            cat <<EOF
  Manual installation: copy or symlink $CANONICAL_DIR into your agent's
  skill directory. The skill expects:
    • cwd = skill directory when commands are invoked
    • bin/optix.sh resolvable from cwd
    • .runtime/ valid (or \$OPTIX_HOME set, or 'optix' on \$PATH)
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
if "$CANONICAL_DIR/bin/optix.sh" watch list >/dev/null 2>&1; then
    echo "✓ Verification passed."
else
    echo "✗ Verification failed — re-running with errors visible:" >&2
    "$CANONICAL_DIR/bin/optix.sh" watch list || true
fi

echo ""
echo "Done. Skill installed for: ${INSTALL_TARGETS[*]}"
echo "  Canonical: $CANONICAL_DIR"
echo "  Runtime:   $CANONICAL_DIR/.runtime"
[[ "$MODE" == "dev" ]] && echo "  (dev mode: edits to $PROJECT_ROOT take effect immediately)"
