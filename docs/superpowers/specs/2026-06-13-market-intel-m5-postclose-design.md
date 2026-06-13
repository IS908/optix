# Market Intel M5 — 收盘后视图 Design Spec

> Status: implementation default approved by epic #120 wording and M5 kickoff (#129).
> Preceded by M1 v0.8.0, M2 v0.9.0, M3 v0.10.0, M4 v0.11.0.
> Branch `feat/v3-m5-postclose` off master -> target **v0.12.0**.

## 1. Goal

Replace the four postclose placeholder slots with real, source-backed components:
earnings quick read, structured event timeline, read-across map, and combined
regular-session plus after-hours movers. Optix remains a pure computation and
storage layer: no LLM calls, no IBKR requirement, no background scheduler. Agent
narrative still flows through the M3 journal.

## 2. Locked Decisions

1. **Scope = one narrow M5**: four postclose cards, one CLI bundle, four HTTP
   endpoints, no new SQLite migration.
2. **Consensus source = yfinance earnings dates**: free/degraded EPS estimate,
   reported EPS, surprise percent, and earnings timestamp when available.
   Revenue consensus and transcript/news ingestion stay out of this release.
3. **Market reaction = yfinance prepost bars**: derive regular-session move,
   after-hours move, and combined day move from raw 5-minute Yahoo bars.
4. **Read-across = sector-rule v1**: reuse the embedded `configs/sectors.json`
   ticker-to-sector map. Large after-hours drivers create peer edges within the
   same sector, with direction, confidence, and lag labels. Deeper edge metadata
   can come later without changing the DTO shape.
5. **Degradation is honest**: every card returns HTTP 200 with warnings when a
   source is missing. Service nil remains 503, matching M4.

## 3. Architecture

Create `internal/postclose`, mirroring M4's `internal/premarket` pattern.

```
internal/postclose/
  dto.go              # JSON contracts shared by CLI and HTTP
  earnings.go         # earnings row filtering and surprise classification
  movers.go           # regular + after-hours + combined move extraction
  read_across.go      # same-sector propagation edges
  timeline.go         # structured event timeline assembly
  service.go          # Service, source interface, bundle aggregation
  adapter.go          # yfinance/raw-bars + embedded sector map adapter
internal/marketdata/
  earnings.go         # yfinance earnings_dates subprocess wrapper and parser
internal/broker/yfinance/fetcher.py
internal/intel/handlers.go
internal/cli/postclose.go
web/src/components/*Postclose*.tsx
```

Dependencies stay acyclic:

`cli` and `intel handlers` -> `postclose` -> `marketdata` + `portfolio.SectorMap`;
`marketdata` -> `broker/yfinance`; `web` consumes HTTP JSON only.

## 4. DTO Contract

```jsonc
earnings: {
  as_of, source, universe_note,
  reports: [{
    symbol, event_time, timing, eps_estimate, eps_reported,
    eps_surprise_pct, surprise_label, stale
  }],
  warnings?
}
timeline: {
  as_of,
  events: [{ ts, symbol, kind, title, detail, severity }],
  warnings?
}
read_across: {
  as_of, sector_source,
  edges: [{
    driver, peer, sector_id, sector_label, direction,
    confidence, lag, driver_after_hours_pct, note
  }],
  warnings?
}
movers: {
  as_of, universe_note,
  gainers: [{ symbol, regular_pct, after_hours_pct, combined_pct, volume, watchlist }],
  losers:  [{ symbol, regular_pct, after_hours_pct, combined_pct, volume, watchlist }],
  warnings?
}
```

`BundleDTO` contains all four cards. Slices are always non-null arrays.

## 5. Methodology

### Earnings Quick Read

Use `Ticker.get_earnings_dates()` via `fetcher.py earnings_dates`. Keep rows
within a window around now: default lookback 14 calendar days and forward 30
calendar days. Classify EPS surprise:

- `beat`: surprise >= +2%
- `miss`: surprise <= -2%
- `inline`: otherwise
- `scheduled`: no reported EPS yet

This is EPS-only and explicitly labeled as a free yfinance consensus proxy.

### Timeline

Build deterministic events from available structured data:

- earnings reports or scheduled earnings
- large after-hours movers
- read-across edges grouped by driver

No external news or transcript parsing in M5. Agent narrative can reference this
timeline through `optix postclose --format json`.

### Read-Across

Resolve sectors through `portfolio.ResolveSectorMap("")`. For each after-hours
driver with `abs(after_hours_pct) >= 2%`, connect to up to three same-sector
peers in the universe. Confidence is `min(0.95, 0.35 + abs(move)/10)`. Lag is
`T+1 open`. This is intentionally transparent and conservative.

### Combined Movers

Fetch raw 5-minute bars with `prepost=True`, derive the latest trading day in
America/New_York:

- previous close: prior trading day's last regular-session close
- regular close: latest day last regular-session close before or at 16:00
- after-hours close: latest day last bar in 16:00-20:00 ET; if absent, use
  regular close and warn

Rank top gainers/losers by absolute combined move, split by sign.

## 6. CLI and HTTP

CLI:

```bash
optix postclose --format text|json
```

It reads the watchlist from SQLite, merges it with a curated liquid universe,
and prints the bundle. Exit behavior follows `premarket`.

HTTP:

```
GET /api/intel/postclose/earnings
GET /api/intel/postclose/timeline
GET /api/intel/postclose/read-across
GET /api/intel/postclose/movers
```

Each endpoint calls only its card method. Source failures return a DTO with
warnings and HTTP 200. Nil service returns 503.

## 7. SPA

Extend `SlotDef.live` with `postclose-earnings`, `postclose-timeline`,
`postclose-read-across`, and `postclose-movers`. Replace the four postclose M5
slots with real components. The UI remains dense and operational: no hero,
cards only for individual slots, no nested cards, no marketing copy.

## 8. Tests

- Go pure function tests for surprise classification, mover extraction,
  read-across edges, timeline assembly.
- `marketdata` parser tests for yfinance earnings payload.
- Service tests for source failure degradation and non-null slices.
- `intel` handler tests for nil 503 and a happy-path endpoint.
- CLI tests for format validation and JSON bundle shape.
- Vitest card tests for rendering data, empty states, and warnings.
- WebUI acceptance test: seed fake postclose source, assert four endpoints and
  `/intel/` render path.

## 9. Non-Goals

- Paid consensus/news vendors.
- Full earnings transcript ingestion or LLM summarization.
- Persistent earnings/news cache.
- Custom read-across graph database.
- Scheduler or cron work.
