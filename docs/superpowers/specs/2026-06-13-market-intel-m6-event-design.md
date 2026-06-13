# Market Intel M6 — 事件日视图 Design Spec

> Status: M6 kickoff approved after #120 roadmap review.
> Preceded by M1 v0.8.0 through M5 v0.12.1.
> Branch `feat/v3-m6-event` off master -> target **v0.13.0**.
> Sub-issue: #132.

## 1. Goal

Replace the four Event view placeholder slots with real, source-backed
components for FOMC/CPI-style event days: rate-path repricing, statement
wording diff, historical event-day patterns, and an asset sensitivity matrix.
Optix remains a pure computation and presentation layer: no LLM calls, no order
entry, no background scheduler requirement in M6 v1.

## 2. Data Source Decision

M6 v1 uses an **IBKR-capable but free-source-first architecture boundary**:

1. Market payloads carry `source`, `basis`, `as_of`, and `warnings` on every
   externally sourced row.
2. The service interface can be backed by an IBKR-aware adapter later, but the
   first implementation uses the existing `marketdata.AssetRef` yfinance router
   because current IBKR broker contracts are stock-symbol oriented and do not
   yet map event assets like SPX/NDX/VIX/US2Y/US10Y/DXY/GOLD into exact IBKR
   index/future/FX contracts.
3. The CLI and HTTP contracts must not imply trading-grade real-time data unless
   a row explicitly reports `source:"ibkr"` and `basis:"live"`.
4. Statement text and event calendars do **not** come from IBKR. FOMC statement
   fixtures and event calendars are local deterministic data in M6 v1; future
   fed.gov/BLS fetchers can replace them without changing the DTO shapes.

This keeps M6 useful without requiring subscriptions, while leaving a clean
upgrade path for IBKR realtime bars/historical bars in a later PR.

## 3. Components

### Rate Path

Show event asset repricing for:

- `US2Y`
- `US10Y`
- `DXY`
- `GOLD`
- `SPX`
- `NDX`
- `VIX`

Each row compares a pre-event baseline to the current quote. The baseline is
computed from available bars when present; otherwise it falls back to quote
change. Rate rows are labeled as yield/price proxy rather than true Fed Funds
futures pricing.

### Statement Diff

Compare two deterministic FOMC statement fixtures:

- prior statement
- current statement

Compute added/removed/unchanged sentence rows and count hawkish/dovish keyword
hits. This is a transparent text diff, not an LLM classification.

### Historical Event Pattern

Use a small built-in event calendar for recent FOMC/CPI dates and yfinance bars
to compute T-1 to T+1 patterns for core event assets. Rows include sample count,
average day move, average next-session move, and a directional consistency
score.

### Sensitivity Matrix

Compute a small matrix from event-window returns:

- rows: SPX, NDX, VIX, DXY, GOLD, US10Y
- columns: risk-on, rates-up, dollar-up

Values are signed sensitivities in [-1, 1] from deterministic return
relationships. This is an event cockpit heuristic, not a fitted risk model.

## 4. CLI and HTTP

CLI:

```bash
optix event --format text|json
```

HTTP:

```text
GET /api/intel/event/rates
GET /api/intel/event/diff
GET /api/intel/event/patterns
GET /api/intel/event/sensitivity
```

Each endpoint returns HTTP 200 with warnings when its source is degraded.
Nil service returns 503, matching M4/M5.

## 5. SPA

Extend `SlotDef.live` with:

- `event-rates`
- `event-diff`
- `event-patterns`
- `event-sensitivity`

Replace the four event M6 placeholders with real cards. The visual treatment
stays dense and operational: compact tables, small badges for source/basis, no
marketing copy, no nested cards.

## 6. Non-Goals

- CME ZQ/Fed Funds futures contract mapping.
- IBKR exact contract mapping for SPX/NDX/VIX/DXY/GOLD in this PR.
- Live FOMC headline/news feed.
- Fed.gov/BLS network fetchers.
- LLM hawkish/dovish classification.
- Automatic phase override or event scheduler persistence.

## 7. Tests

- Pure Go tests for rate repricing, statement diff, event pattern aggregation,
  and sensitivity scoring.
- Service tests for per-card degradation and non-null slices.
- HTTP tests for nil 503 and happy-path event endpoints.
- CLI tests for command registration and JSON shape.
- Vitest tests for the four Event cards.
- WebUI acceptance test wiring `/api/intel/event/*` and `/intel/`.

