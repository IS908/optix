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

    test -L "$home/.agents/skills/optix/.runtime" || fail "runtime must be a versioned symlink"
    test -f "$home/.agents/skills/optix/.runtime/configs/portfolio.yaml" ||
        fail "installer did not copy portfolio.yaml into .runtime/configs"
    test -f "$home/.agents/skills/optix/.runtime/configs/sectors.json" ||
        fail "installer did not copy sectors.json into .runtime/configs"
    local first second external="$home/user-data/optix.db"
    first="$(cd "$home/.agents/skills/optix/.runtime" && pwd -P)"
    HOME="$home" PATH="$fakebin:$PATH" "$bundle/install.sh" --agent generic >/dev/null
    second="$(cd "$home/.agents/skills/optix/.runtime" && pwd -P)"
    [[ "$first" != "$second" && -d "$first" ]] || fail "upgrade must retain previous runtime"
    HOME="$home" PATH="$fakebin:$PATH" "$bundle/install.sh" --rollback >/dev/null
    [[ "$(cd "$home/.agents/skills/optix/.runtime" && pwd -P)" == "$first" ]] || fail "rollback failed"
    mkdir -p "$first/data" "$(dirname "$external")"
    printf 'legacy records' > "$first/data/optix.db"
    if HOME="$home" PATH="$fakebin:$PATH" "$bundle/install.sh" --agent generic >/dev/null 2>&1; then
        fail "unmigrated runtime data allowed a layout switch"
    fi
    [[ "$(cd "$home/.agents/skills/optix/.runtime" && pwd -P)" == "$first" ]] || fail "failed install switched runtime"
    cp "$first/data/optix.db" "$external"
    HOME="$home" OPTIX_DB_PATH="$external" PATH="$fakebin:$PATH" "$bundle/install.sh" --agent generic >/dev/null
    cmp "$external" "$first/data/optix.db" || fail "upgrade changed legacy/user data"
    grep -qx "$first/data/optix.db" "$home/.agents/skills/optix/.runtime/legacy-databases" || fail "retained legacy location not recorded"
    rm "$first/STORAGE_LAYOUT_V1"
    if HOME="$home" OPTIX_DB_PATH="$external" PATH="$fakebin:$PATH" "$bundle/install.sh" --rollback >/dev/null 2>&1; then
        fail "unsafe legacy rollback accepted"
    fi
    mv "$first/data/optix.db" "$first/data/journal.sqlite"
    mkdir -p "$home/.claude/skills/optix/data"
    printf 'legacy agent database' > "$home/.claude/skills/optix/data/journal.sqlite"
    if HOME="$home" PATH="$fakebin:$PATH" "$bundle/install.sh" --uninstall --agent claude >/dev/null 2>&1; then
        fail "uninstall removed real legacy agent directory"
    fi
    test -f "$home/.claude/skills/optix/data/journal.sqlite" || fail "legacy agent data lost"

    if HOME="$home" PATH="$fakebin:$PATH" "$bundle/install.sh" --uninstall --purge --agent generic >/dev/null 2>&1; then
        fail "purge deleted retained legacy data"
    fi
    test -f "$external" && test -f "$first/data/journal.sqlite" || fail "purge lost data"

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

test_source_install_is_independent_and_dev_explicit() {
    local source="$TMPDIR/source" fakebin="$TMPDIR/source-tools" home="$TMPDIR/source-home"
    mkdir -p "$source/.git" "$source/bin" "$source/python/.venv/bin" "$source/python/src" "$source/skills/commands/optix" "$source/configs" "$fakebin" "$home"
    touch "$source/Makefile"
    cp "$ROOT/skills/commands/optix/"{install.sh,SKILL.md,skill-wrapper.sh,optix.sh} "$source/skills/commands/optix/"
    cp "$ROOT/python/pyproject.toml" "$source/python/pyproject.toml"
    printf '#!/bin/sh\necho fixture-version\n' > "$source/bin/optix"
    chmod +x "$source/bin/optix"
    write_fake_python "$fakebin"
    ln -s "$fakebin/python3.14" "$source/python/.venv/bin/python"
    printf '#!/bin/sh\necho fixture-version\n' > "$fakebin/git"
    cat > "$fakebin/go" <<'GO'
#!/bin/bash
while [[ $# -gt 0 ]]; do
    if [[ "$1" == -o ]]; then cp bin/optix "$2"; exit; fi
    shift
done
exit 1
GO
    chmod +x "$fakebin/go" "$fakebin/git"
    HOME="$home" PATH="$fakebin:$PATH" "$source/skills/commands/optix/install.sh" --agent generic >/dev/null
    local independent
    independent="$(readlink "$home/.agents/skills/optix/.runtime")"
    [[ "$independent" != "$source" ]] || fail "source install silently selected dev mode"
    mv "$source" "$source-moved"
    HOME="$home" "$home/.agents/skills/optix/bin/optix.sh" --version | grep -q fixture-version || fail "moving source broke independent install"
    HOME="$home" PATH="$fakebin:$PATH" "$source-moved/skills/commands/optix/install.sh" --dev --agent generic >/dev/null
    [[ "$(readlink "$home/.agents/skills/optix/.runtime")" == "$source-moved" ]] || fail "explicit dev mode ignored"
    HOME="$home" PATH="$fakebin:$PATH" "$source-moved/skills/commands/optix/install.sh" --agent generic >/dev/null
    [[ "$(readlink "$home/.agents/skills/optix/.runtime")" != "$source-moved" ]] || fail "dev to standalone switch failed"
}

test_real_cli_migration_and_fresh_environment() {
    local runtime="$TMPDIR/real-runtime" home="$TMPDIR/real-home" old="$TMPDIR/old-data.db" target="$TMPDIR/external-data.db"
    local name="optix-skill-v0.0.0-test-$(go env GOOS)-$(go env GOARCH)"
    mkdir -p "$runtime/bin" "$home"
    tar -xzf "$TMPDIR/dist/$name.tar.gz" -C "$TMPDIR" "$name/bin/optix"
    cp "$TMPDIR/$name/bin/optix" "$runtime/bin/optix"
    local cli="$runtime/bin/optix"
    "$cli" --db "$old" watch add AAPL >/dev/null
    "$cli" data migrate --from "$old" --to "$target" > "$TMPDIR/migration.json"
    printf '%s\n' "$old" > "$runtime/legacy-databases"
    HOME="$home" XDG_DATA_HOME="$home/user-data" OPTIX_DB_PATH= "$cli" --config '' data status > "$TMPDIR/status.json"
    grep -q 'legacy database' "$TMPDIR/status.json" || fail "fresh shell did not report retained database"
    if HOME="$home" XDG_DATA_HOME="$home/user-data" OPTIX_DB_PATH= "$cli" --config '' watch list >/dev/null 2>&1; then
        fail "fresh shell silently selected empty database"
    fi
    test ! -e "$home/user-data/optix/optix.db" || fail "fresh shell created empty database"
    HOME="$home" OPTIX_DB_PATH="$target" "$cli" --config '' watch list | grep -q AAPL || fail "explicit migrated DB lost records"
    if "$cli" data migrate --from "$old" --to "$target" >/dev/null 2>&1; then fail "repeat migration overwrote target"; fi
}

test_malformed_legacy_config_restores_active_runtime() {
    local bundle="$TMPDIR/malformed-bundle" home="$TMPDIR/malformed-home" fakebin="$TMPDIR/fakebin"
    cp -R "$TMPDIR/bundle" "$bundle"
    cp "$TMPDIR/real-runtime/bin/optix" "$bundle/bin/optix"
    local legacy="$home/.agents/skills/optix/.runtime"
    mkdir -p "$legacy/configs" "$legacy/bin"
    printf 'database: [invalid yaml' > "$legacy/configs/optix.yaml"
    printf '#!/bin/sh\necho old-version\n' > "$legacy/bin/optix"
    chmod +x "$legacy/bin/optix"
    if HOME="$home" OPTIX_DB_PATH= PATH="$fakebin:$PATH" "$bundle/install.sh" --agent generic >/dev/null 2>&1; then
        fail "malformed legacy YAML accepted"
    fi
    [[ -d "$legacy" && ! -L "$legacy" ]] || fail "failed legacy upgrade did not restore runtime directory"
    "$legacy/bin/optix" --version | grep -q old-version || fail "old runtime no longer runnable"
    grep -q 'invalid yaml' "$legacy/configs/optix.yaml" || fail "failed install modified legacy config"
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

test_source_install_is_independent_and_dev_explicit
test_build_release_includes_configs
test_real_cli_migration_and_fresh_environment
test_release_installer_copies_configs
test_malformed_legacy_config_restores_active_runtime
test_release_installer_fails_when_optix_engine_install_fails
test_skill_wrapper_adds_analysis_addr_for_python_commands
test_skill_wrapper_routes_market_intel_ibkr_probe

echo "release packaging smoke tests passed"
