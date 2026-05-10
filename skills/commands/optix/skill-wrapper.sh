#!/usr/bin/env bash
#
# optix skill-wrapper: thin shim installed at <skill_dir>/bin/optix.sh that
# discovers the optix project root and forwards to the project's optix.sh.
#
# Resolution order:
#   1. $OPTIX_HOME environment variable (if set and points to a valid repo)
#   2. <skill_dir>/optix-repo symlink (created by install.sh)
#   3. error out with a clear message
#
# This file is agent-agnostic: it works for any skill system that places SKILL.md
# next to a bin/ subdirectory and invokes commands with cwd = skill root.

set -euo pipefail

# bin/optix.sh -> SKILL_DIR is one directory up
SKILL_DIR="$(cd "$(dirname "$0")/.." && pwd)"

# 1. Try OPTIX_HOME
PROJECT_ROOT=""
if [[ -n "${OPTIX_HOME:-}" && -f "${OPTIX_HOME}/skills/commands/optix/optix.sh" ]]; then
    PROJECT_ROOT="$OPTIX_HOME"
fi

# 2. Try the symlink in the skill directory
if [[ -z "$PROJECT_ROOT" && -e "$SKILL_DIR/optix-repo" ]]; then
    candidate="$(cd "$SKILL_DIR/optix-repo" 2>/dev/null && pwd -P)" || candidate=""
    if [[ -n "$candidate" && -f "$candidate/skills/commands/optix/optix.sh" ]]; then
        PROJECT_ROOT="$candidate"
    fi
fi

# 3. Bail with actionable error
if [[ -z "$PROJECT_ROOT" ]]; then
    cat >&2 <<EOF
ERROR: optix project not found.

The skill at $SKILL_DIR could not locate the optix repository. Either:
  • Set the OPTIX_HOME environment variable to your optix checkout, OR
  • Recreate the symlink: ln -sfn /path/to/optix "$SKILL_DIR/optix-repo"

Tip: re-run the installer from a fresh optix checkout:
  bash /path/to/optix/skills/commands/optix/install.sh --agent <claude|openclaw|hermes>
EOF
    exit 1
fi

# Forward to the project's wrapper. exec replaces this process so signal handling
# and exit codes pass through unchanged.
exec bash "$PROJECT_ROOT/skills/commands/optix/optix.sh" "$@"
