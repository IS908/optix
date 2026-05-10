#!/usr/bin/env bash
#
# Skill entry point — installed at <skill_dir>/bin/optix.sh.
#
# Resolves the optix runtime (Go binary + Python engine) and forwards the
# command. The runtime can live in one of three places, checked in order:
#
#   1. $OPTIX_HOME                          — developer override (source checkout)
#   2. <skill_dir>/.runtime                  — release-mode bundle (or dev symlink)
#   3. $(command -v optix)                   — system install on $PATH (Homebrew, etc.)
#
# Both layouts (source checkout and .runtime/) expose the same internal paths:
#   <runtime>/bin/optix
#   <runtime>/python/.venv/bin/python
#   <runtime>/data/optix.db
#   <runtime>/skills/commands/optix/optix.sh   (orchestration script)
#
# This means the same wrapper works in dev and release modes without any
# branching logic.

set -euo pipefail

SKILL_DIR="$(cd "$(dirname "$0")/.." && pwd)"

# --- Resolve runtime ---------------------------------------------------------
RUNTIME=""

# 1. Explicit override via OPTIX_HOME
if [[ -n "${OPTIX_HOME:-}" && -f "$OPTIX_HOME/skills/commands/optix/optix.sh" ]]; then
    RUNTIME="$OPTIX_HOME"
fi

# 2. Bundled .runtime/ inside the skill directory (most users)
if [[ -z "$RUNTIME" && -e "$SKILL_DIR/.runtime" ]]; then
    candidate="$(cd "$SKILL_DIR/.runtime" 2>/dev/null && pwd -P)" || candidate=""
    if [[ -n "$candidate" && -f "$candidate/skills/commands/optix/optix.sh" ]]; then
        RUNTIME="$candidate"
    fi
fi

# 3. System-wide install (e.g. Homebrew puts `optix` on PATH).
#    In this mode we trust `optix` to know its own paths and skip
#    the orchestration script entirely.
if [[ -z "$RUNTIME" ]] && command -v optix >/dev/null 2>&1; then
    exec optix "$@"
fi

if [[ -z "$RUNTIME" ]]; then
    cat >&2 <<EOF
ERROR: optix runtime not found.

The skill at $SKILL_DIR could not locate the optix runtime. Set up one of:

  • Set OPTIX_HOME to a source checkout:
      export OPTIX_HOME=/path/to/optix

  • Install the release bundle:
      curl -L https://github.com/IS908/optix/releases/latest/download/optix-skill-\$(uname -s | tr '[:upper:]' '[:lower:]')-\$(uname -m).tar.gz | tar xz
      cd optix-skill-* && ./install.sh --agent claude

  • Recreate the .runtime symlink (dev mode):
      ln -snf /path/to/optix "$SKILL_DIR/.runtime"

EOF
    exit 1
fi

# Forward to the runtime's orchestration script (which handles Python server,
# IBKR connection probe, port resolution, etc.). exec replaces this process so
# signals and exit codes pass through.
exec bash "$RUNTIME/skills/commands/optix/optix.sh" "$@"
