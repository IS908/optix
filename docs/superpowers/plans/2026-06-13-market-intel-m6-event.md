# Market Intel M6 — 事件日视图 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Market Intel Event view with four source-backed M6 cards plus CLI and HTTP access.

**Architecture:** Add `internal/eventintel` as a pure-compute package mirroring `premarket` and `postclose`. It consumes a small source interface backed by yfinance in v1, carries explicit source/basis labels, and exposes DTOs through `optix event`, `/api/intel/event/*`, and React cards.

**Tech Stack:** Go, Cobra, net/http, existing `internal/marketdata` yfinance router, React + TypeScript + Vitest.

Spec: `docs/superpowers/specs/2026-06-13-market-intel-m6-event-design.md`

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/eventintel/dto.go` | Create | Shared JSON DTOs |
| `internal/eventintel/source.go` | Create | Source interface, yfinance adapter, fixtures |
| `internal/eventintel/rates.go` | Create | Rate-path repricing pure compute |
| `internal/eventintel/diff.go` | Create | Statement sentence diff + keyword scoring |
| `internal/eventintel/patterns.go` | Create | Historical event pattern aggregation |
| `internal/eventintel/sensitivity.go` | Create | Event sensitivity matrix |
| `internal/eventintel/service.go` | Create | Card methods + bundle |
| `internal/intel/handlers.go` | Modify | Register four `/api/intel/event/*` endpoints |
| `internal/cli/event.go` | Create | `optix event` command |
| `internal/cli/root.go` | Modify | Register `event` command |
| `internal/cli/server.go` | Modify | Attach Event service to Intel handlers |
| `web/src/api/types.ts` | Modify | Event DTO types |
| `web/src/components/Event*.tsx` | Create | Four Event cards + tests |
| `web/src/views/slots.ts` | Modify | Live event slots |
| `web/src/views/SlotGrid.tsx` | Modify | Dispatch Event cards |
| `internal/webui/event_acceptance_test.go` | Create | WebUI/HTTP acceptance |
| `CHANGELOG.md`, `CLAUDE.md`, `AGENTS.md`, `skills/commands/optix/SKILL.md` | Modify | Release/docs |

---

## Tasks

### Task 1: DTOs and Pure Computes

- [x] Write failing Go tests for rate repricing, statement diff, historical pattern aggregation, and sensitivity scoring.
- [x] Add `internal/eventintel/dto.go`, `rates.go`, `diff.go`, `patterns.go`, and `sensitivity.go`.
- [x] Run `go test ./internal/eventintel -count=1` and confirm pass.

### Task 2: Source Adapter and Service

- [x] Write failing service tests for source degradation and non-null slices.
- [x] Add `internal/eventintel/source.go` with `MarketSource`, built-in statement/event fixtures, and yfinance adapter.
- [x] Add `internal/eventintel/service.go` with `Rates`, `Diff`, `Patterns`, `Sensitivity`, and `Bundle`.
- [x] Run `go test ./internal/eventintel -count=1` and confirm pass.

### Task 3: HTTP and CLI

- [x] Write failing `internal/intel` handler tests for event nil 503 and one happy path.
- [x] Add `Event *eventintel.Service` to `intel.Handlers` and register four event endpoints.
- [x] Write failing CLI tests for `newEventCmd` and JSON bundle shape.
- [x] Add `internal/cli/event.go`; register it in `root.go`; wire event service in `server.go`.
- [x] Run `go test ./internal/intel ./internal/cli -count=1` and confirm pass.

### Task 4: SPA Event Cards

- [x] Add Event DTOs to `web/src/api/types.ts`.
- [x] Write Vitest tests for four cards.
- [x] Add `EventRatesCard`, `EventDiffCard`, `EventPatternsCard`, and `EventSensitivityCard`.
- [x] Wire `slots.ts` and `SlotGrid.tsx`.
- [x] Run `cd web && npm test` and confirm pass.

### Task 5: Acceptance, Docs, and Release

- [x] Add `internal/webui/event_acceptance_test.go` with fake event service.
- [x] Update `CHANGELOG.md`, `CLAUDE.md`, `AGENTS.md`, and `skills/commands/optix/SKILL.md`.
- [x] Run `go test ./... -count=1`, `go vet ./...`, `cd web && npm test`, and `cd web && npm run build -- --outDir /tmp/optix-web-dist-m6-verify`.
- [x] Start local server and capture `/intel/` Event view screenshot.
- [x] Self-review diff and fix bugs with tests first.
- [x] Commit, push, open PR closing #132, merge, tag `v0.13.0`, create GitHub Release.
