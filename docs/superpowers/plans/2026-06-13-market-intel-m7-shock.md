# Market Intel M7 — 突发冲击视图 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Market Intel Shock view with four M7 cards plus CLI and HTTP access.

**Architecture:** Add `internal/shockintel` as a pure-compute package mirroring `eventintel`. It consumes a small source interface that is IBKR-preferred by contract and yfinance/local-fixture backed in v1, then exposes DTOs through `optix shock`, `/api/intel/shock/*`, and React cards.

**Tech Stack:** Go, Cobra, net/http, existing `internal/marketdata` yfinance source, React + TypeScript + Vitest.

Spec: `docs/superpowers/specs/2026-06-13-market-intel-m7-shock-design.md`

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/shockintel/dto.go` | Create | Shared JSON DTOs and source-facing structs |
| `internal/shockintel/source.go` | Create | Source interface, yfinance adapter, local analog templates |
| `internal/shockintel/regime.go` | Create | Regime trigger scoring and mechanism trigger DTO |
| `internal/shockintel/fingerprint.go` | Create | Supply/demand/liquidity/policy fingerprint scoring |
| `internal/shockintel/analogs.go` | Create | Historical analog similarity matching |
| `internal/shockintel/liquidity.go` | Create | ETF spread/depth liquidity state |
| `internal/shockintel/service.go` | Create | Card methods + bundle |
| `internal/intel/handlers.go` | Modify | Register four `/api/intel/shock/*` endpoints |
| `internal/cli/shock.go` | Create | `optix shock` command |
| `internal/cli/root.go` | Modify | Register `shock` command |
| `internal/cli/server.go` | Modify | Attach Shock service to Intel handlers |
| `web/src/api/types.ts` | Modify | Shock DTO types |
| `web/src/components/Shock*.tsx` | Create | Four Shock cards + tests |
| `web/src/views/slots.ts` | Modify | Live shock slots |
| `web/src/views/SlotGrid.tsx` | Modify | Dispatch Shock cards |
| `internal/webui/shock_acceptance_test.go` | Create | WebUI/HTTP acceptance |
| `CHANGELOG.md`, `CLAUDE.md`, `AGENTS.md`, `skills/commands/optix/SKILL.md` | Modify | Release/docs |

---

## Tasks

### Task 1: DTOs and Pure Computes

- [ ] Write failing Go tests in `internal/shockintel` for regime scoring, fingerprint classification, analog similarity, and liquidity state.
- [ ] Add `dto.go`, `regime.go`, `fingerprint.go`, `analogs.go`, and `liquidity.go`.
- [ ] Run `go test ./internal/shockintel -count=1` and confirm pass.

### Task 2: Source Adapter and Service

- [ ] Write failing service tests for yfinance fallback, depth degradation, option-metric degradation, and non-null slices.
- [ ] Add `source.go` with `Source`, `YFinanceAdapter`, IBKR-preferred metadata labels, and local analog templates.
- [ ] Add `service.go` with `Regime`, `Fingerprint`, `Analogs`, `Liquidity`, and `Bundle`.
- [ ] Run `go test ./internal/shockintel -count=1` and confirm pass.

### Task 3: HTTP and CLI

- [ ] Write failing `internal/intel` handler tests for shock nil 503 and one happy path.
- [ ] Add `Shock *shockintel.Service` to `intel.Handlers` and register four shock endpoints.
- [ ] Write failing CLI tests for `newShockCmd` and JSON bundle shape.
- [ ] Add `internal/cli/shock.go`; register it in `root.go`; wire shock service in `server.go`.
- [ ] Run `go test ./internal/intel ./internal/cli -count=1` and confirm pass.

### Task 4: SPA Shock Cards

- [ ] Add Shock DTOs to `web/src/api/types.ts`.
- [ ] Write Vitest tests for four cards.
- [ ] Add `ShockRegimeCard`, `ShockFingerprintCard`, `ShockAnalogsCard`, and `ShockLiquidityCard`.
- [ ] Wire `slots.ts` and `SlotGrid.tsx`.
- [ ] Run `cd web && npm test` and confirm pass.

### Task 5: Acceptance, Docs, and Release

- [ ] Add `internal/webui/shock_acceptance_test.go` with fake shock service coverage.
- [ ] Update `CHANGELOG.md`, `CLAUDE.md`, `AGENTS.md`, and `skills/commands/optix/SKILL.md`.
- [ ] Run `go test ./... -count=1`, `go vet ./...`, `cd web && npm test`, `cd web && npm run build -- --outDir /tmp/optix-web-dist-m7-verify`, and `make build`.
- [ ] Start local server and capture `/intel/` Shock view screenshot.
- [ ] Self-review diff and fix bugs with tests first.
- [ ] Commit, push, open PR closing #134, merge, tag `v0.14.0`, and create GitHub Release.
