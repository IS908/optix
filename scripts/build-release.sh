#!/usr/bin/env bash
#
# build-release.sh — assemble a single platform tarball.
#
# Inputs (env or argv):
#   VERSION  — release version, e.g. v1.2.0  (no default; required)
#   GOOS     — target OS:   darwin | linux
#   GOARCH   — target arch: amd64  | arm64
#   OUT_DIR  — output directory (default: dist/)
#
# Output:
#   $OUT_DIR/optix-skill-$VERSION-$GOOS-$GOARCH.tar.gz
#
# The tarball mirrors the layout install.sh expects in release mode:
#   optix-skill-$VERSION-$GOOS-$GOARCH/
#   ├── install.sh
#   ├── SKILL.md
#   ├── skill-wrapper.sh
#   ├── bin/optix                                  # cross-compiled
#   ├── python/{pyproject.toml, src/optix_engine/}
#   └── skills/commands/optix/optix.sh

set -euo pipefail

# --- Resolve project root via this script's location -------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Inputs ------------------------------------------------------------------
VERSION="${VERSION:-}"
GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
OUT_DIR="${OUT_DIR:-$PROJECT_ROOT/dist}"

if [[ -z "$VERSION" ]]; then
    echo "ERROR: VERSION is required (e.g. VERSION=v1.2.0)" >&2
    exit 2
fi

case "$GOOS" in darwin|linux) ;; *) echo "ERROR: unsupported GOOS=$GOOS"; exit 2;; esac
case "$GOARCH" in amd64|arm64) ;; *) echo "ERROR: unsupported GOARCH=$GOARCH"; exit 2;; esac

NAME="optix-skill-${VERSION}-${GOOS}-${GOARCH}"
STAGE="$OUT_DIR/.stage/$NAME"
TARBALL="$OUT_DIR/${NAME}.tar.gz"

echo "==> Building $NAME"
echo "    Project: $PROJECT_ROOT"
echo "    Stage:   $STAGE"
echo "    Output:  $TARBALL"

# --- Clean staging area ------------------------------------------------------
rm -rf "$STAGE"
mkdir -p "$STAGE/bin" "$STAGE/python" "$STAGE/skills/commands/optix"

# --- Cross-compile the Go binary --------------------------------------------
# Pure-Go, no CGO (modernc.org/sqlite). Strip with -s -w to slim the binary.
echo "==> Compiling bin/optix (GOOS=$GOOS GOARCH=$GOARCH, CGO_ENABLED=0)"
(
    cd "$PROJECT_ROOT"
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
        go build -trimpath -ldflags="-s -w -X main.version=$VERSION" \
        -o "$STAGE/bin/optix" ./cmd/optix-cli
)
chmod +x "$STAGE/bin/optix"

# --- Python source -----------------------------------------------------------
echo "==> Copying Python engine source"
cp "$PROJECT_ROOT/python/pyproject.toml" "$STAGE/python/pyproject.toml"
cp -r "$PROJECT_ROOT/python/src" "$STAGE/python/src"
# Strip caches from Python source if any
find "$STAGE/python/src" -type d -name '__pycache__' -exec rm -rf {} + 2>/dev/null || true
find "$STAGE/python/src" -name '*.pyc' -delete 2>/dev/null || true

# --- Skill files (top-level) -------------------------------------------------
echo "==> Copying skill files"
cp "$PROJECT_ROOT/skills/commands/optix/SKILL.md"          "$STAGE/SKILL.md"
cp "$PROJECT_ROOT/skills/commands/optix/skill-wrapper.sh"  "$STAGE/skill-wrapper.sh"
cp "$PROJECT_ROOT/skills/commands/optix/install.sh"        "$STAGE/install.sh"
chmod +x "$STAGE/skill-wrapper.sh" "$STAGE/install.sh"

# Orchestration script (lives at the runtime layout's expected path).
cp "$PROJECT_ROOT/skills/commands/optix/optix.sh"          "$STAGE/skills/commands/optix/optix.sh"
chmod +x "$STAGE/skills/commands/optix/optix.sh"

# --- Tar it up ---------------------------------------------------------------
mkdir -p "$OUT_DIR"
echo "==> Creating $TARBALL"
# -C $OUT_DIR/.stage so the tarball contains $NAME/ as the single root entry.
tar -czf "$TARBALL" -C "$OUT_DIR/.stage" "$NAME"

# --- Verify (size + listing) -------------------------------------------------
SIZE=$(du -h "$TARBALL" | cut -f1)
echo ""
echo "✓ Built $TARBALL ($SIZE)"
echo "  Top-level entries:"
tar -tzf "$TARBALL" | awk -F/ '{print $1"/"$2}' | sort -u | sed 's/^/    /'

# --- Cleanup stage (keep the tarball, drop the unpacked tree) ----------------
rm -rf "$STAGE"
