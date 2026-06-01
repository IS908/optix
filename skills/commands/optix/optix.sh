#!/usr/bin/env bash
set -euo pipefail

# PROJECT_ROOT (a.k.a. RUNTIME) is derived from this script's physical location:
#   <runtime>/skills/commands/optix/optix.sh  →  PROJECT_ROOT = <runtime>
#
# This script is identical between source checkouts and release-mode .runtime/
# bundles, so the same logic applies in both. Avoids `git rev-parse`, which
# fails when the runtime is a tarball-extracted directory without .git, or
# when called with a cwd outside the project.
SCRIPT_PATH="${BASH_SOURCE[0]}"
while [[ -L "$SCRIPT_PATH" ]]; do
    DIR="$(cd -P "$(dirname "$SCRIPT_PATH")" && pwd)"
    SCRIPT_PATH="$(readlink "$SCRIPT_PATH")"
    [[ "$SCRIPT_PATH" != /* ]] && SCRIPT_PATH="$DIR/$SCRIPT_PATH"
done
PROJECT_ROOT="$(cd "$(dirname "$SCRIPT_PATH")/../../.." && pwd)"

# Skill uses a dedicated port to avoid conflict with local dev servers (50052)
ANALYSIS_PORT="${OPTIX_ANALYSIS_PORT:-50053}"
ANALYSIS_ADDR="localhost:${ANALYSIS_PORT}"

# Auto-build if the binary is missing — only when a Makefile is present (dev mode).
# Release-mode runtimes have a prebuilt binary and no Makefile; if the binary
# went missing there, we fail loudly instead of trying to build the impossible.
if [[ ! -x "$PROJECT_ROOT/bin/optix" ]]; then
    if [[ -f "$PROJECT_ROOT/Makefile" ]] && command -v make >/dev/null 2>&1; then
        echo "Building optix CLI..." >&2
        make -C "$PROJECT_ROOT" build >&2
    else
        echo "ERROR: $PROJECT_ROOT/bin/optix not found and no Makefile to rebuild it." >&2
        echo "  Reinstall the skill: ./install.sh --agent <name>" >&2
        exit 1
    fi
fi

# --- Check IBKR TWS/Gateway for commands that need live data ---
IB_HOST="${OPTIX_IB_HOST:-127.0.0.1}"
IB_PORT="${OPTIX_IB_PORT:-gateway}"

# Resolve port alias to number for nc -z connectivity check
resolve_port() {
    case "$(echo "$1" | tr '[:upper:]' '[:lower:]')" in
        gateway) echo 4001 ;; tws) echo 7496 ;; *) echo "$1" ;;
    esac
}
IB_PORT_NUM=$(resolve_port "$IB_PORT")

case "${1:-}" in
    quote|analyze|dashboard|chain)
        if ! nc -z "$IB_HOST" "$IB_PORT_NUM" 2>/dev/null; then
            echo "ℹ️  IBKR TWS/Gateway not detected at ${IB_HOST}:${IB_PORT_NUM} — will use Yahoo Finance (delayed data)" >&2
        fi
        ;;
    positions|trades)
        if ! nc -z "$IB_HOST" "$IB_PORT_NUM" 2>/dev/null; then
            echo "⚠️  IBKR TWS/Gateway not detected at ${IB_HOST}:${IB_PORT_NUM} — account data requires IBKR (Yahoo Finance has no account API). The command will error out." >&2
        fi
        ;;
esac

# --- Determine if command needs Python gRPC server ---
PY_SERVER_PID=""
READY_FILE=""
NEED_PY_SERVER=false
EXTRA_ARGS=()

case "${1:-}" in
    analyze|dashboard|max-pain)
        NEED_PY_SERVER=true
        EXTRA_ARGS+=(--analysis-addr "$ANALYSIS_ADDR")
        ;;
    portfolio)
        case "${2:-}" in
            greeks|stress)
                NEED_PY_SERVER=true
                EXTRA_ARGS+=(--analysis-addr "$ANALYSIS_ADDR")
                ;;
        esac
        ;;
esac

if [[ "$NEED_PY_SERVER" == true ]]; then
    if ! nc -z localhost "$ANALYSIS_PORT" 2>/dev/null; then
        READY_FILE=$(mktemp -t optix-ready.XXXXXX)
        rm -f "$READY_FILE"  # remove so we can detect when Python creates it

        echo "Starting Python analysis server on port ${ANALYSIS_PORT}..." >&2
        "$PROJECT_ROOT/python/.venv/bin/python" -m optix_engine.grpc_server.server \
            --addr="$ANALYSIS_ADDR" --ready-file="$READY_FILE" &>/dev/null &
        PY_SERVER_PID=$!

        # Wait for the ready-file signal (written by Python after server.start()).
        # This is faster and more reliable than TCP polling with nc -z:
        # file-exists check is ~0.1ms vs nc connection attempt ~5-50ms,
        # and it confirms the server is fully initialized, not just listening.
        for i in {1..600}; do
            if [[ -f "$READY_FILE" ]]; then
                echo "Python analysis server ready." >&2
                break
            fi
            if ! kill -0 "$PY_SERVER_PID" 2>/dev/null; then
                echo "ERROR: Python analysis server process exited unexpectedly" >&2
                rm -f "$READY_FILE"
                exit 1
            fi
            sleep 0.2
        done
        if [[ ! -f "$READY_FILE" ]]; then
            echo "ERROR: Python analysis server failed to start within 120s" >&2
            kill "$PY_SERVER_PID" 2>/dev/null
            rm -f "$READY_FILE"
            exit 1
        fi
    fi
fi

# --- Cleanup on exit: stop Python server if we started it ---
# IMPORTANT: this function MUST return 0. Bash propagates the EXIT trap's
# return code as the script's exit code, so a stray non-zero from a failing
# `[[ ... ]]` test would corrupt the real exit status (e.g. `watch list`
# would appear to fail even when it succeeded).
cleanup() {
    if [[ -n "$PY_SERVER_PID" ]]; then
        kill "$PY_SERVER_PID" 2>/dev/null || true
        wait "$PY_SERVER_PID" 2>/dev/null || true
    fi
    if [[ -n "$READY_FILE" ]]; then
        rm -f "$READY_FILE"
    fi
    return 0
}
trap cleanup EXIT

cd "$PROJECT_ROOT"
"$PROJECT_ROOT/bin/optix" --db "$PROJECT_ROOT/data/optix.db" --python "$PROJECT_ROOT/python/.venv/bin/python" --ib-host "$IB_HOST" --ib-port "$IB_PORT" "$@" ${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"}
