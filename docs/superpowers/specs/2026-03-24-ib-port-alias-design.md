# IB Port Alias Design

**Date**: 2026-03-24
**Status**: Approved
**Goal**: Replace numeric port memorization with semantic aliases for `--ib-port`.

## Problem

Users must remember four IBKR port numbers (7496/7497/4001/4002). The most common use case (IB Gateway live) requires overriding the default every time.

## Design

### Alias Mapping

| Alias     | Port | Description          |
|-----------|------|----------------------|
| `gateway` | 4001 | IB Gateway live      |
| `tws`     | 7496 | TWS live             |
| (number)  | as-is | Direct port number  |

Default changes from `7496` (TWS) to `gateway` (4001).

Paper trading ports (4002, 7497) remain numeric-only — no alias needed per user decision.

### Changes

**1. `internal/cli/root.go`**

- `ibPortRaw` becomes `string` with default `"gateway"`
- New function `resolveIBPort(raw string) (int, error)`: maps `gateway`->4001, `tws`->7496, else `strconv.Atoi`
- `PersistentPreRunE` calls `resolveIBPort`, stores result in `ibPort int`
- Flag help text: `IB port: gateway (4001), tws (7496), or number`

**2. `skills/commands/optix/optix.sh`**

- Default: `IB_PORT="${OPTIX_IB_PORT:-gateway}"`
- Add `resolve_port()` shell function for `nc -z` connectivity check (needs numeric port)
- Pass `$IB_PORT` as-is to Go binary (Go handles alias resolution)

**3. `skills/commands/optix/install.sh` (`write_standalone_optix_sh`)**

- Same changes as optix.sh: default `gateway`, add `resolve_port()` for `nc -z`

**4. Documentation**

- `CLAUDE.md`: Update default port references and IBKR Configuration section

### Not Changed

- `internal/broker/ibkr/client.go` — `Config.Port` stays `int`, receives resolved value
- `internal/webui/`, `broker_pool.go` — only consume resolved `int`
- `configs/optix.yaml` — not updated (user decision)
