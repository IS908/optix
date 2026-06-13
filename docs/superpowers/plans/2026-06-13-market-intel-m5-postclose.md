# Market Intel M5 — 收盘后视图 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace postclose placeholder slots with a real postclose bundle: earnings quick read, event timeline, read-across edges, and combined regular plus after-hours movers.

**Architecture:** Mirror M4. Add an `internal/postclose` package with pure computation and a small service; expose it through `optix postclose`, `/api/intel/postclose/*`, and four React cards. Use yfinance earnings dates and raw 5-minute prepost bars. No SQLite migration.

**Tech Stack:** Go, Cobra, net/http, yfinance subprocess, embedded sector map via `internal/portfolio`, React + TypeScript + Vitest.

Spec: `docs/superpowers/specs/2026-06-13-market-intel-m5-postclose-design.md`

---

## Tasks

### Task 1: Marketdata Earnings Wrapper

- [ ] Write failing parser tests for `parseRawEarnings`.
- [ ] Add `internal/marketdata/earnings.go` with `EarningsEvent`, `RawEarnings`, and parser.
- [ ] Extend `internal/broker/yfinance/fetcher.py` with `earnings_dates`.
- [ ] Run `go test ./internal/marketdata -run Earnings -count=1`.

### Task 2: Postclose Pure Compute

- [ ] Write failing tests for EPS surprise classification.
- [ ] Write failing tests for regular/after-hours mover extraction.
- [ ] Write failing tests for same-sector read-across edges.
- [ ] Write failing tests for timeline ordering.
- [ ] Add `internal/postclose/{dto,earnings,movers,read_across,timeline}.go`.
- [ ] Run `go test ./internal/postclose -count=1`.

### Task 3: Postclose Service and Adapter

- [ ] Write failing service degradation tests.
- [ ] Add `internal/postclose/service.go` and `adapter.go`.
- [ ] Ensure all DTO slices are non-null.
- [ ] Run `go test ./internal/postclose -count=1`.

### Task 4: HTTP and CLI

- [ ] Write failing `internal/intel` handler tests for nil 503 and one happy path.
- [ ] Add postclose endpoints to `internal/intel/handlers.go`.
- [ ] Write failing CLI test for JSON output shape.
- [ ] Add `internal/cli/postclose.go` and register it in `root.go` and `server.go`.
- [ ] Run `go test ./internal/intel ./internal/cli -count=1`.

### Task 5: SPA Cards

- [ ] Add TypeScript DTOs.
- [ ] Write Vitest tests for the four postclose cards.
- [ ] Add four components and wire `SlotGrid` live dispatch.
- [ ] Run `cd web && npm test`.

### Task 6: Acceptance, Docs, Release Notes

- [ ] Add WebUI postclose acceptance test with fake source.
- [ ] Update `CHANGELOG.md`, `CLAUDE.md`, `AGENTS.md`, and `skills/commands/optix/SKILL.md`.
- [ ] Run `go test ./... -count=1`, targeted race tests, `go vet ./...`, `cd web && npm test`, `cd web && npm run build`.
- [ ] Start local server and capture `/intel/` postclose screenshot.
- [ ] Self-review diff; fix any bugs with failing tests first.
- [ ] Commit, push, open PR closing #129, merge, tag `v0.12.0`, create GitHub Release.
