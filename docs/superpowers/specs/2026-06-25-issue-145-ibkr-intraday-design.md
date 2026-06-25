# Issue #145: IBKR-First Intraday Movers and Sector Heatmap Design

## Context

Issue #145 tracks the remaining Market Intel intraday gap: the WebUI no longer
shows placeholder intraday cards, but it still lacks real intraday movers and
sector heatmap data. The selected direction is **B: IBKR-first**.

Current relevant constraints:

- `internal/broker.Broker` already exposes `GetQuote` and `GetHistoricalBars`.
- IBKR quote reads return bid/ask/last/close and are already wrapped by the
  fallback broker used by the web server.
- `marketdata` pulse intentionally remains macro/index oriented and should not
  become a per-equity intraday scanner in this slice.
- Existing Market Intel cards degrade with explicit `warnings`, non-nil slices,
  `source`, `basis`, and `as_of` metadata.

## Scope

This feature adds a first production intraday implementation, not a full market
scanner platform.

In scope:

- Backend service package `internal/intraday`.
- HTTP endpoints:
  - `GET /api/intel/intraday/movers`
  - `GET /api/intel/intraday/sector-heatmap`
- WebUI cards:
  - `IntradayMoversCard`
  - `IntradaySectorHeatmapCard`
- Intraday view slots for those two cards plus the existing judgment workflow.
- Unit tests for ranking, sector aggregation, endpoint wiring, and WebUI render
  contracts.

Out of scope for this PR:

- IBKR market scanner subscriptions.
- User-configurable intraday universe.
- Streaming WebSocket updates.
- New persistence tables.
- Trading actions or order placement.

## Universe

The first version uses:

- watchlist symbols, marked with `watchlist: true`;
- the same built-in liquid equity/ETF seed list used by other Intel mover
  cards, normalized and de-duplicated.

This keeps behavior deterministic and testable. IBKR scanner support can later
replace or augment the universe without changing the WebUI DTO contract.

## Data Source Strategy

The service is IBKR-first:

1. Use the configured broker for quotes and 5-minute historical bars. In the web
   server this broker is the existing IBKR-with-yfinance-fallback broker pool.
2. Determine each row's `source` and `basis` from quote/bar availability:
   - `ibkr` / `realtime` when IBKR quote data is available;
   - fallback source labels when the active broker has degraded;
   - `missing` rows are omitted from ranked mover arrays but warnings explain
     the missing symbols.
3. The WebUI must show source/freshness metadata and warnings so users can tell
   whether the reading is live or degraded.

No endpoint should silently return static placeholder rows.

## Movers Calculation

For each symbol:

- Fetch the latest quote.
- Fetch recent 5-minute bars covering the current US regular session.
- Compute:
  - `last`: quote mark/last/close fallback from the quote;
  - `open`: first regular-session 5-minute bar open for the current trading day;
  - `pct`: `(last - open) / open * 100`;
  - `volume`: sum of same-day regular-session bar volume;
  - `basis`: `realtime`, `delayed`, or `degraded` based on the source metadata
    available from the broker path.
- Rank gainers descending by `pct` and losers ascending by `pct`; cap both lists
  at 8 rows.

If a symbol has no usable session open or latest price, skip it and add a
warning. If all symbols are missing, return empty arrays plus warnings.

## Sector Heatmap Calculation

The heatmap reuses the same computed mover rows so it does not refetch market
data.

For each sector from the embedded sector map:

- group mover rows by `sector_id`;
- compute `avg_pct`, `sample_n`, `gainers`, `losers`, and `top_symbol`;
- sort sectors by absolute `avg_pct` descending, then `sector_id`;
- include `sector_source` so users can see which mapping produced the grouping.

Rows with unknown sectors are grouped under `unclassified` with the existing
sector label behavior.

## API DTOs

`IntradayMoversDTO`:

- `as_of`
- `source`
- `basis`
- `universe_note`
- `gainers`
- `losers`
- `warnings`

Mover row:

- `symbol`
- `source`
- `basis`
- `as_of`
- `last`
- `open`
- `pct`
- `volume`
- `watchlist`

`IntradaySectorHeatmapDTO`:

- `as_of`
- `source`
- `basis`
- `sector_source`
- `rows`
- `warnings`

Sector row:

- `sector_id`
- `sector_label`
- `avg_pct`
- `sample_n`
- `gainers`
- `losers`
- `top_symbol`

## WebUI

The intraday view renders:

1. Intraday movers card.
2. Sector heatmap card.
3. Existing judgment journal workflow.

The cards must:

- show loading states;
- render empty/degraded states without crashing;
- show warnings via `DataWarnings`;
- show `source`/`basis` in compact metadata;
- avoid placeholder explanatory copy that suggests unimplemented functionality.

## Testing

Backend tests:

- ranking splits gainers/losers and caps rows;
- missing bars/quotes return warnings and non-nil arrays;
- sector heatmap groups by sector and sorts by absolute average move;
- handlers register both `/api/intel/intraday/*` endpoints.

Frontend tests:

- intraday view renders both new cards;
- movers card renders gainers/losers and warnings;
- sector heatmap card renders sector rows and warnings;
- no placeholder `盘中异动`/`板块热力` `SlotCard` is shown.

Validation before PR merge:

- `go test ./internal/intraday ./internal/intel`
- `go test ./...`
- `npm test`
- `npm run build`
- `python/.venv/bin/python -m pytest python/tests/ -q`
- `make build`
- WebUI E2E screenshot of intraday view.

## Release

Because this is a user-visible Market Intel feature, merge as a feature PR and
cut a minor or patch release according to the repository's current release
cadence. The changelog must mention the new IBKR-first intraday movers and
sector heatmap endpoints.
