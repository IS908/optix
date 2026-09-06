#!/usr/bin/env bash
#
# install.sh — install the optix skill for one or more agents.
#
# Default: independent runtime under .runtimes/, with atomic .runtime pointer.
# --dev opts into a source symlink. --rollback selects the previous runtime.
# Persistent data lives outside the skill; legacy runtime databases are guarded.
# Python environments remain at their original version directory for rollback.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

CANONICAL_DIR="$HOME/.agents/skills/optix"
AGENT=""
UNINSTALL=false
PURGE=false
DEV=false
ROLLBACK=false

# MODE/PROJECT_ROOT are detected lazily — only required when installing.
# For --uninstall we operate purely on $CANONICAL_DIR and per-agent symlinks,
# so we don't need a source tree or tarball at all (which lets users uninstall
# from the install.sh copy stashed at $CANONICAL_DIR/install.sh long after the
# original tarball is gone).
MODE=""
PROJECT_ROOT=""

detect_mode() {
    if [[ -f "$SCRIPT_DIR/../../../Makefile" && -d "$SCRIPT_DIR/../../../.git" ]]; then
        MODE="source"
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
Usage: install.sh [--agent <list>] [--dev | --rollback] [--uninstall] [--purge]

Options:
  --agent <list>   Comma-separated agents (claude, openclaw, hermes, generic).
                   If omitted, auto-detects which agents are configured.
  --dev            Explicitly link a source checkout (default: independent runtime).
  --rollback       Swap to the previous installed runtime.
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
        --dev) DEV=true; shift ;;
        --rollback) ROLLBACK=true; shift ;;
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
# Atomic symlink replacement differs between BSD and GNU mv.
replace_link() {
    local value="$1" dest="$2" next="${2}.next.$$"
    ln -s "$value" "$next"
    if [[ "$(uname -s)" == Darwin ]]; then mv -fh "$next" "$dest"; else mv -fT "$next" "$dest"; fi
}

check_legacy_data() {
    local old="${1:-$CANONICAL_DIR/.runtime}"
    if [[ -e "$old/data/optix.db" || -e "$old/data/optix.db-wal" ]]; then
        # Do not infer successful migration from the presence of a second DB.
        # Require the operator to select their migrated, external data path.
        if [[ -z "${OPTIX_DB_PATH:-}" || ! -f "$OPTIX_DB_PATH" || "$OPTIX_DB_PATH" != /* ]]; then
            echo "ERROR: legacy runtime data exists. Migrate it first and set absolute OPTIX_DB_PATH to the verified external database." >&2
            exit 1
        fi
        local selected oldreal canonicalreal
        selected="$(cd "$(dirname "$OPTIX_DB_PATH")" && pwd -P)/$(basename "$OPTIX_DB_PATH")"
        oldreal="$(cd "$old" && pwd -P)"
        canonicalreal="$(cd "$CANONICAL_DIR" && pwd -P)"
        case "$selected" in "$oldreal"/*|"$canonicalreal"/*) echo "ERROR: selected database must be outside the old runtime and skill directory" >&2; exit 1;; esac
        [[ -L "$OPTIX_DB_PATH" ]] && { echo "ERROR: use the physical migrated database path" >&2; exit 1; }
    fi
    return 0
}

capture_config_database() {
    local old="$1" target="$2"
    if [[ -f "$old/configs/optix.yaml" ]]; then
        # Resolve existing YAML with the new CLI before changing directories.
        # This records custom database filenames/locations, not just optix.db.
        (cd "$old" && OPTIX_DB_PATH= "$target/bin/optix" --config "$old/configs/optix.yaml" data status) > "$target/.legacy-status.json"
        "$target/python/.venv/bin/python" -c 'import json,sys; print(json.load(open(sys.argv[1]))["database"]["path"])' "$target/.legacy-status.json" >> "$target/legacy-databases"
        rm "$target/.legacy-status.json"
    fi
}

activate_runtime() {
    local target="$1" old=""
    if [[ -L "$CANONICAL_DIR/.runtime" ]]; then
        old="$(cd "$CANONICAL_DIR/.runtime" && pwd -P)"
    elif [[ -d "$CANONICAL_DIR/.runtime" ]]; then
        # Preserve the complete legacy bundle; never rm -rf user runtime data.
        old="$CANONICAL_DIR/.runtimes/legacy-$(date +%s)-$$"
        mv "$CANONICAL_DIR/.runtime" "$old"
    fi
    if [[ -n "$old" ]]; then replace_link "$old" "$CANONICAL_DIR/.previous-runtime"; fi
    if [[ -n "$old" && -f "$target/STORAGE_LAYOUT_V1" ]]; then
        # Carry old locations forward so a fresh shell cannot silently create
        # an empty default DB after the install-time environment disappears.
        capture_config_database "$old" "$target"
        local records
        records="$(mktemp "$target/.legacy-XXXXXXXX")"
        { [[ ! -f "$target/legacy-databases" ]] || cat "$target/legacy-databases"; [[ ! -f "$old/legacy-databases" ]] || cat "$old/legacy-databases"; printf '%s\n' "$old/data/optix.db"; } | sort -u > "$records"
        mv "$records" "$target/legacy-databases"
    fi
    replace_link "$target" "$CANONICAL_DIR/.runtime"
}

build_runtime() {
    mkdir -p "$CANONICAL_DIR/.runtimes"
    if [[ "$DEV" == true ]]; then
        [[ "$MODE" == source ]] || { echo "ERROR: --dev requires a source checkout" >&2; exit 1; }
        [[ -x "$PROJECT_ROOT/bin/optix" && -x "$PROJECT_ROOT/python/.venv/bin/python" ]] || { echo "ERROR: build source CLI and Python venv first" >&2; exit 1; }
        "$PROJECT_ROOT/bin/optix" data status >/dev/null
        READY_RUNTIME="$PROJECT_ROOT"
        return
    fi
    # Allocate the final path before creating the venv: venv entrypoints contain
    # absolute paths and must never be renamed after installation.
    READY_RUNTIME="$(mktemp -d "$CANONICAL_DIR/.runtimes/runtime-XXXXXXXX")"
    local target="$READY_RUNTIME"
    mkdir -p "$target/bin" "$target/python" "$target/configs" "$target/skills/commands/optix"
    if [[ "$MODE" == source ]]; then
        local version
        version="$(git -C "$PROJECT_ROOT" describe --tags --always --dirty)"
        (cd "$PROJECT_ROOT" && go build -trimpath -ldflags="-X main.version=$version" -o "$target/bin/optix" ./cmd/optix-cli)
    else
        cp "$PROJECT_ROOT/bin/optix" "$target/bin/optix"
    fi
    chmod +x "$target/bin/optix"
    cp -r "$PROJECT_ROOT/python/src" "$target/python/src"
    cp "$PROJECT_ROOT/python/pyproject.toml" "$target/python/pyproject.toml"
    for config in portfolio.yaml sectors.json; do
        [[ ! -f "$PROJECT_ROOT/configs/$config" ]] || cp "$PROJECT_ROOT/configs/$config" "$target/configs/$config"
    done
    cp "$PROJECT_ROOT/skills/commands/optix/optix.sh" "$target/skills/commands/optix/optix.sh"
    chmod +x "$target/skills/commands/optix/optix.sh"
    create_release_venv "$target/python"
    "$target/bin/optix" --version > "$target/VERSION"
    "$target/bin/optix" data status >/dev/null
    (cd "$target" && pwd -P) > "$target/STORAGE_LAYOUT_V1"
    if [[ "$MODE" == source ]]; then capture_config_database "$PROJECT_ROOT" "$target"; printf '%s\n' "$PROJECT_ROOT/data/optix.db" >> "$target/legacy-databases"; fi
}

# ---------------------------------------------------------------------------
# Build an isolated Python venv inside each immutable runtime directory.
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

    # A runtime owns its engine and dependencies. Installing into host Python
    # would make a later upgrade silently change an older runtime on rollback.
    "$PYTHON_BIN" -m venv "$PY_TARGET/.venv"
    "$PY_TARGET/.venv/bin/pip" install --quiet "$PY_TARGET"

    if ! "$PY_TARGET/.venv/bin/python" -c "import optix_engine" 2>/dev/null; then
        echo "  ERROR: Python runtime cannot import optix_engine" >&2
        return 1
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

    activate_runtime "$READY_RUNTIME"
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
        if [[ ! -L "$agent_dir" ]]; then
            echo "ERROR: refusing to remove legacy agent directory $agent_dir; archive it explicitly" >&2
            return 1
        fi
        rm -f "$agent_dir"
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
mkdir -p "$CANONICAL_DIR"
LOCK_DIR="$CANONICAL_DIR/.install-lock"
mkdir "$LOCK_DIR" 2>/dev/null || { echo "ERROR: installation already in progress (or stale .install-lock)" >&2; exit 1; }
READY_RUNTIME=""
ACTIVATED=false
cleanup_install() {
    if [[ "$ACTIVATED" == false && "$READY_RUNTIME" == "$CANONICAL_DIR/.runtimes/"* ]]; then rm -rf "$READY_RUNTIME"; fi
    rmdir "$LOCK_DIR" 2>/dev/null || true
}
trap cleanup_install EXIT
if [[ "$UNINSTALL" == true ]]; then
    for target in "${INSTALL_TARGETS[@]}"; do
        echo ""
        echo "--- Uninstalling for: $target ---"
        uninstall_agent "$target"
    done
    if [[ "$PURGE" == true ]]; then
        echo ""
        echo "--- Purging canonical install ---"
        # Explicit --db accepts arbitrary filenames. Inspect the SQLite header
        # as well as common suffixes; never trust only the default filename.
        while IFS= read -r -d '' candidate; do
            case "$candidate" in *.db|*.sqlite|*.sqlite3|*-wal|*-journal)
                echo "ERROR: refusing purge: potential user database $candidate" >&2; exit 1;; esac
            header="$(od -An -tx1 -N16 "$candidate" | tr -d ' \n')"
            if [[ "$header" == 53514c69746520666f726d6174203300 ]]; then
                echo "ERROR: refusing purge: SQLite database $candidate" >&2; exit 1
            fi
        done < <(find "$CANONICAL_DIR" -type f -print0)
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
check_legacy_data
if [[ "$ROLLBACK" == true ]]; then
    previous="$(readlink "$CANONICAL_DIR/.previous-runtime")"
    [[ -x "$previous/bin/optix" ]] || { echo "ERROR: no runnable previous runtime" >&2; exit 1; }
    [[ -f "$previous/STORAGE_LAYOUT_V1" && "$(cat "$previous/STORAGE_LAYOUT_V1")" == "$(cd "$previous" && pwd -P)" ]] || { echo "ERROR: previous runtime predates safe storage layout (or is dev mode); reinstall a compatible version explicitly" >&2; exit 1; }
    activate_runtime "$previous"
    ACTIVATED=true
    "$CANONICAL_DIR/bin/optix.sh" --version
    exit 0
fi
detect_mode
if [[ "$MODE" == source && "$DEV" == false ]]; then check_legacy_data "$PROJECT_ROOT"; fi
build_runtime
install_canonical
ACTIVATED=true

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
if "$CANONICAL_DIR/bin/optix.sh" data status >/dev/null 2>&1; then
    echo "✓ Verification passed."
else
    echo "✗ Verification failed — re-running with errors visible:" >&2
    "$CANONICAL_DIR/bin/optix.sh" data status || true
fi

echo ""
echo "Done. Skill installed for: ${INSTALL_TARGETS[*]}"
echo "  Canonical: $CANONICAL_DIR"
echo "  Runtime:   $CANONICAL_DIR/.runtime"
# Trailing `|| true` keeps the script's exit code at 0 in release mode.
# Without it, `set -e` propagates the failed `[[ ... ]]` test as the script's
# final exit status — install was successful but `$?` would be 1, which is
# misleading to callers (and breaks chained shell commands).
[[ "$DEV" == true ]] && echo "  (dev mode: edits to $PROJECT_ROOT take effect immediately)" || true
