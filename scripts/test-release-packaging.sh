#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

assert_tar_entry() {
    local tarball="$1"
    local entry="$2"
    tar -tzf "$tarball" | grep -qx "$entry" || fail "missing tar entry: $entry"
}

write_fake_python() {
    local fakebin="$1"
    cat >"$fakebin/python3.14" <<'PY'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "--version" ]]; then
    echo "Python 3.14.0"
    exit 0
fi

if [[ "${1:-}" == "-c" ]]; then
    case "${2:-}" in
        *sys.version_info*) exit 0 ;;
        *optix_engine*) exit 0 ;;
        *) exit 1 ;;
    esac
fi

if [[ "${1:-}" == "-m" && "${2:-}" == "venv" ]]; then
    target="$3"
    mkdir -p "$target/bin"
    cat >"$target/bin/pip" <<'PIP'
#!/usr/bin/env bash
exit 0
PIP
    chmod +x "$target/bin/pip"
    ln -sf "$(command -v python3.14)" "$target/bin/python"
    exit 0
fi

exit 0
PY
    chmod +x "$fakebin/python3.14"
}

write_fake_host_python_with_failing_pip() {
    local fakebin="$1"
    cat >"$fakebin/python3.14" <<'PY'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "--version" ]]; then
    echo "Python 3.14.0"
    exit 0
fi

if [[ "${1:-}" == "-c" ]]; then
    case "${2:-}" in
        *sys.version_info*) exit 0 ;;
        *numpy*) exit 0 ;;
        *optix_engine*) exit 1 ;;
        *) exit 0 ;;
    esac
fi

if [[ "${1:-}" == "-m" && "${2:-}" == "pip" ]]; then
    exit 1
fi

exit 0
PY
    chmod +x "$fakebin/python3.14"
}

write_fake_host_python_with_working_pip() {
    local fakebin="$1"
    cat >"$fakebin/python3.14" <<'PY'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "--version" ]]; then
    echo "Python 3.14.0"
    exit 0
fi

if [[ "${1:-}" == "-c" ]]; then
    case "${2:-}" in
        *sys.version_info*) exit 0 ;;
        *numpy*) exit 0 ;;
        *optix_engine*) test -f "${FAKE_OPTIX_ENGINE_MARKER:?}" ;;
        *) exit 0 ;;
    esac
fi

if [[ "${1:-}" == "-m" && "${2:-}" == "pip" ]]; then
    printf '%s\n' "$*" >"${FAKE_OPTIX_ENGINE_PIP_ARGS:?}"
    case "$*" in
        *" -e "*) touch "${FAKE_OPTIX_ENGINE_MARKER:?}" ;;
    esac
    exit 0
fi

exit 0
PY
    chmod +x "$fakebin/python3.14"
}

test_build_release_includes_configs() {
    local out="$TMPDIR/dist"
    local version="v0.0.0-test"
    local goos goarch name tarball

    goos="$(go env GOOS)"
    goarch="$(go env GOARCH)"
    name="optix-skill-${version}-${goos}-${goarch}"
    tarball="$out/${name}.tar.gz"

    OUT_DIR="$out" VERSION="$version" GOOS="$goos" GOARCH="$goarch" \
        "$ROOT/scripts/build-release.sh" >/dev/null

    assert_tar_entry "$tarball" "$name/configs/portfolio.yaml"
    assert_tar_entry "$tarball" "$name/configs/sectors.json"
}

test_release_installer_copies_configs() {
    local bundle="$TMPDIR/bundle"
    local fakebin="$TMPDIR/fakebin"
    local home="$TMPDIR/home"

    mkdir -p "$bundle/bin" "$bundle/python/src" "$bundle/skills/commands/optix" "$bundle/configs" "$fakebin" "$home"
    cp "$ROOT/skills/commands/optix/install.sh" "$bundle/install.sh"
    cp "$ROOT/skills/commands/optix/SKILL.md" "$bundle/SKILL.md"
    cp "$ROOT/skills/commands/optix/skill-wrapper.sh" "$bundle/skill-wrapper.sh"
    cp "$ROOT/skills/commands/optix/optix.sh" "$bundle/skills/commands/optix/optix.sh"
    cp "$ROOT/python/pyproject.toml" "$bundle/python/pyproject.toml"
    cp "$ROOT/configs/portfolio.yaml" "$bundle/configs/portfolio.yaml"
    cp "$ROOT/configs/sectors.json" "$bundle/configs/sectors.json"
    touch "$bundle/bin/optix"
    chmod +x "$bundle/install.sh" "$bundle/skill-wrapper.sh" "$bundle/skills/commands/optix/optix.sh" "$bundle/bin/optix"

    write_fake_python "$fakebin"

    HOME="$home" PATH="$fakebin:$PATH" "$bundle/install.sh" --agent generic >/dev/null

    test -f "$home/.agents/skills/optix/.runtime/configs/portfolio.yaml" ||
        fail "installer did not copy portfolio.yaml into .runtime/configs"
    test -f "$home/.agents/skills/optix/.runtime/configs/sectors.json" ||
        fail "installer did not copy sectors.json into .runtime/configs"
}

test_release_installer_fails_when_optix_engine_install_fails() {
    local bundle="$TMPDIR/bad-python-bundle"
    local fakebin="$TMPDIR/bad-python-fakebin"
    local home="$TMPDIR/bad-python-home"

    mkdir -p "$bundle/bin" "$bundle/python/src" "$bundle/skills/commands/optix" "$bundle/configs" "$fakebin" "$home"
    cp "$ROOT/skills/commands/optix/install.sh" "$bundle/install.sh"
    cp "$ROOT/skills/commands/optix/SKILL.md" "$bundle/SKILL.md"
    cp "$ROOT/skills/commands/optix/skill-wrapper.sh" "$bundle/skill-wrapper.sh"
    cp "$ROOT/skills/commands/optix/optix.sh" "$bundle/skills/commands/optix/optix.sh"
    cp "$ROOT/python/pyproject.toml" "$bundle/python/pyproject.toml"
    cp "$ROOT/configs/portfolio.yaml" "$bundle/configs/portfolio.yaml"
    cp "$ROOT/configs/sectors.json" "$bundle/configs/sectors.json"
    cat >"$bundle/bin/optix" <<'OPTIX'
#!/usr/bin/env bash
exit 0
OPTIX
    chmod +x "$bundle/install.sh" "$bundle/skill-wrapper.sh" "$bundle/skills/commands/optix/optix.sh" "$bundle/bin/optix"

    write_fake_host_python_with_failing_pip "$fakebin"

    if HOME="$home" PATH="$fakebin:$PATH" "$bundle/install.sh" --agent generic >/dev/null 2>&1; then
        fail "installer succeeded even though optix_engine could not be installed"
    fi
}

test_release_installer_accepts_host_python_when_engine_imports() {
    local bundle="$TMPDIR/good-host-python-bundle"
    local fakebin="$TMPDIR/good-host-python-fakebin"
    local home="$TMPDIR/good-host-python-home"
    local marker="$TMPDIR/good-host-python-installed"
    local pip_args="$TMPDIR/good-host-python-pip-args"

    mkdir -p "$bundle/bin" "$bundle/python/src" "$bundle/skills/commands/optix" "$bundle/configs" "$fakebin" "$home"
    cp "$ROOT/skills/commands/optix/install.sh" "$bundle/install.sh"
    cp "$ROOT/skills/commands/optix/SKILL.md" "$bundle/SKILL.md"
    cp "$ROOT/skills/commands/optix/skill-wrapper.sh" "$bundle/skill-wrapper.sh"
    cp "$ROOT/skills/commands/optix/optix.sh" "$bundle/skills/commands/optix/optix.sh"
    cp "$ROOT/python/pyproject.toml" "$bundle/python/pyproject.toml"
    cp "$ROOT/configs/portfolio.yaml" "$bundle/configs/portfolio.yaml"
    cp "$ROOT/configs/sectors.json" "$bundle/configs/sectors.json"
    cat >"$bundle/bin/optix" <<'OPTIX'
#!/usr/bin/env bash
exit 0
OPTIX
    chmod +x "$bundle/install.sh" "$bundle/skill-wrapper.sh" "$bundle/skills/commands/optix/optix.sh" "$bundle/bin/optix"

    write_fake_host_python_with_working_pip "$fakebin"

    HOME="$home" PATH="$fakebin:$PATH" FAKE_OPTIX_ENGINE_MARKER="$marker" FAKE_OPTIX_ENGINE_PIP_ARGS="$pip_args" \
        "$bundle/install.sh" --agent generic >/dev/null
    test -L "$home/.agents/skills/optix/.runtime/python/.venv/bin/python" ||
        fail "host-Python path did not create thin shim python symlink"
    test -f "$marker" || fail "host-Python path did not install optix_engine"
    grep -q -- "-e" "$pip_args" || fail "host-Python path did not invoke editable optix_engine install"
}

test_skill_wrapper_adds_analysis_addr_for_python_commands() {
    local runtime="$TMPDIR/runtime"
    local fakebin="$TMPDIR/fake-nc"
    local argsfile="$TMPDIR/args.txt"
    local cwdfile="$TMPDIR/cwd.txt"

    mkdir -p "$runtime/bin" "$runtime/python/.venv/bin" "$runtime/skills/commands/optix" "$runtime/data" "$fakebin"
    cp "$ROOT/skills/commands/optix/optix.sh" "$runtime/skills/commands/optix/optix.sh"
    chmod +x "$runtime/skills/commands/optix/optix.sh"

    cat >"$runtime/bin/optix" <<EOF
#!/usr/bin/env bash
pwd >"$cwdfile"
printf '%s\n' "\$@" >"$argsfile"
EOF
    chmod +x "$runtime/bin/optix"

    cat >"$runtime/python/.venv/bin/python" <<'PY'
#!/usr/bin/env bash
echo "unexpected python server start" >&2
exit 99
PY
    chmod +x "$runtime/python/.venv/bin/python"

    cat >"$fakebin/nc" <<'NC'
#!/usr/bin/env bash
case "$*" in
    *localhost*) exit 0 ;;
    *) exit 1 ;;
esac
NC
    chmod +x "$fakebin/nc"

    PATH="$fakebin:$PATH" OPTIX_ANALYSIS_PORT=59999 \
        "$runtime/skills/commands/optix/optix.sh" max-pain AAPL --source yfinance
    grep -qx -- "--analysis-addr" "$argsfile" || fail "max-pain missing --analysis-addr"
    grep -qx -- "localhost:59999" "$argsfile" || fail "max-pain missing skill analysis addr"

    PATH="$fakebin:$PATH" OPTIX_ANALYSIS_PORT=59999 \
        "$runtime/skills/commands/optix/optix.sh" portfolio greeks --net-liq-usd 100000
    grep -qx -- "--analysis-addr" "$argsfile" || fail "portfolio greeks missing --analysis-addr"
    grep -qx -- "localhost:59999" "$argsfile" || fail "portfolio greeks missing skill analysis addr"

    PATH="$fakebin:$PATH" OPTIX_ANALYSIS_PORT=59999 \
        "$runtime/skills/commands/optix/optix.sh" portfolio stress --net-liq-usd 100000
    grep -qx -- "--analysis-addr" "$argsfile" || fail "portfolio stress missing --analysis-addr"
    grep -qx -- "localhost:59999" "$argsfile" || fail "portfolio stress missing skill analysis addr"
    grep -qx -- "$runtime" "$cwdfile" || fail "skill wrapper did not run optix from runtime cwd"
}

test_skill_wrapper_routes_market_intel_ibkr_probe() {
    local runtime="$TMPDIR/runtime-market-intel"
    local fakebin="$TMPDIR/fake-market-intel-bin"
    local argsfile="$TMPDIR/market-intel-args.txt"
    local ncfile="$TMPDIR/market-intel-nc.txt"

    mkdir -p "$runtime/bin" "$runtime/python/.venv/bin" "$runtime/skills/commands/optix" "$runtime/data" "$fakebin"
    cp "$ROOT/skills/commands/optix/optix.sh" "$runtime/skills/commands/optix/optix.sh"
    chmod +x "$runtime/skills/commands/optix/optix.sh"

    cat >"$runtime/bin/optix" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$@" >"$argsfile"
EOF
    chmod +x "$runtime/bin/optix"

    cat >"$runtime/python/.venv/bin/python" <<'PY'
#!/usr/bin/env bash
echo "unexpected python server start" >&2
exit 99
PY
    chmod +x "$runtime/python/.venv/bin/python"

    cat >"$fakebin/nc" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >>"$ncfile"
exit 1
EOF
    chmod +x "$fakebin/nc"

    PATH="$fakebin:$PATH" OPTIX_IB_PORT=4001 \
        "$runtime/skills/commands/optix/optix.sh" shock --format json >/dev/null 2>"$TMPDIR/shock-stderr.txt"
    grep -qx -- "-z 127.0.0.1 4001" "$ncfile" || fail "shock should probe IBKR availability"
    grep -q -- "shock will use yfinance/degraded fallback" "$TMPDIR/shock-stderr.txt" ||
        fail "shock missing degraded fallback warning"
    grep -qx -- "shock" "$argsfile" || fail "shock command was not forwarded"

    : >"$ncfile"
    PATH="$fakebin:$PATH" OPTIX_IB_PORT=4001 \
        "$runtime/skills/commands/optix/optix.sh" premarket --format json >/dev/null 2>"$TMPDIR/premarket-stderr.txt"
    test ! -s "$ncfile" || fail "premarket should not probe IBKR"
    grep -qx -- "premarket" "$argsfile" || fail "premarket command was not forwarded"
}

test_build_release_includes_configs
test_release_installer_copies_configs
test_release_installer_fails_when_optix_engine_install_fails
test_release_installer_accepts_host_python_when_engine_imports
test_skill_wrapper_adds_analysis_addr_for_python_commands
test_skill_wrapper_routes_market_intel_ibkr_probe

echo "release packaging smoke tests passed"
