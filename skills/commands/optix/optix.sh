#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(git -C "$(dirname "$0")" rev-parse --show-toplevel)" && pwd)"

# Skill uses a dedicated port to avoid conflict with local dev servers (50052)
ANALYSIS_PORT="${OPTIX_ANALYSIS_PORT:-50053}"
ANALYSIS_ADDR="localhost:${ANALYSIS_PORT}"

# Auto-build if binary missing
if [[ ! -x "$PROJECT_ROOT/bin/optix" ]]; then
    echo "Building optix CLI..." >&2
    make -C "$PROJECT_ROOT" build >&2
fi

# --- Check IBKR TWS/Gateway for commands that need live data ---
IB_HOST="${OPTIX_IB_HOST:-127.0.0.1}"
IB_PORT="${OPTIX_IB_PORT:-7496}"

case "${1:-}" in
    quote|analyze|dashboard|chain)
        if ! nc -z "$IB_HOST" "$IB_PORT" 2>/dev/null; then
            echo "ℹ️  IBKR TWS/Gateway not detected at ${IB_HOST}:${IB_PORT} — will use Yahoo Finance (delayed data, no options)" >&2
        fi
        ;;
esac

# --- Determine if command needs Python gRPC server ---
PY_SERVER_PID=""
READY_FILE=""
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
cleanup() {
    if [[ -n "$PY_SERVER_PID" ]]; then
        kill "$PY_SERVER_PID" 2>/dev/null
        wait "$PY_SERVER_PID" 2>/dev/null
    fi
    [[ -n "$READY_FILE" ]] && rm -f "$READY_FILE"
}
trap cleanup EXIT

"$PROJECT_ROOT/bin/optix" --db "$PROJECT_ROOT/data/optix.db" --python "$PROJECT_ROOT/python/.venv/bin/python" --ib-host "$IB_HOST" --ib-port "$IB_PORT" "$@" ${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"}
