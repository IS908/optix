#!/bin/bash
# Generate Go and Python code from protobuf definitions.
#
# Prerequisites:
#   go install github.com/bufbuild/buf/cmd/buf@latest
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
#   pip install grpcio-tools  (already in pyproject.toml)
#
# Usage: ./scripts/proto-gen.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROTO_DIR="$ROOT/proto"
GO_OUT="$ROOT/gen/go"
PY_OUT="$ROOT/python/src/optix_engine/gen"

echo "=== Proto code generation ==="
echo "Proto dir: $PROTO_DIR"

# --- Go generation (via buf) ---
echo ""
echo "--- Generating Go code ---"

export PATH="$HOME/go/bin:$PATH"

if command -v buf &>/dev/null; then
    cd "$ROOT"
    mkdir -p "$GO_OUT"
    buf dep update proto
    buf generate
    echo "Go code generated in $GO_OUT"
else
    echo "WARNING: buf not installed, skipping Go generation"
    echo "Install: go install github.com/bufbuild/buf/cmd/buf@latest"
fi

# --- Python generation (via grpc_tools.protoc) ---
echo ""
echo "--- Generating Python code ---"

# Find python with grpc_tools
PYTHON=""
if [ -f "$ROOT/python/.venv/bin/python" ]; then
    PYTHON="$ROOT/python/.venv/bin/python"
elif command -v python3 &>/dev/null; then
    PYTHON="python3"
fi

if [ -z "$PYTHON" ]; then
    echo "ERROR: No Python found"
    exit 1
fi

# Create output directories
mkdir -p "$PY_OUT/optix/marketdata/v1"
mkdir -p "$PY_OUT/optix/analysis/v1"

# Generate Python pb2 and grpc files
PROTO_FILES=(
    "optix/marketdata/v1/types.proto"
    "optix/marketdata/v1/marketdata.proto"
    "optix/analysis/v1/types.proto"
    "optix/analysis/v1/analysis.proto"
)

for proto in "${PROTO_FILES[@]}"; do
    echo "  Generating: $proto"
    $PYTHON -m grpc_tools.protoc \
        -I"$PROTO_DIR" \
        --python_out="$PY_OUT" \
        --grpc_python_out="$PY_OUT" \
        --pyi_out="$PY_OUT" \
        "$PROTO_DIR/$proto"
done

# Create __init__.py files for generated sub-packages (skip the root gen/ which has a custom one)
find "$PY_OUT/optix" -type d -exec touch {}/__init__.py \;

# Write the root gen/__init__.py with sys.path fixup (generated code uses absolute imports)
cat > "$PY_OUT/__init__.py" << 'PYEOF'
"""Generated protobuf/gRPC code. Importing this package fixes sys.path for proto imports."""
import sys
from pathlib import Path

_gen_dir = str(Path(__file__).parent)
if _gen_dir not in sys.path:
    sys.path.insert(0, _gen_dir)
PYEOF

echo "Python code generated in $PY_OUT"

echo ""
echo "=== Done ==="
