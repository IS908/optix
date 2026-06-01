# Trade Journal JSON Schemas (Agent Contract)

Stable schemas for `optix journal … --format json` and the `/api/journal/*`
HTTP endpoints. Adding fields is non-breaking. Renaming or removing fields
requires a versioned bump.

## Exit codes (CLI)

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Generic error (bad flags, internal error) |
| 2 | IBKR unreachable |
| 3 | SQLite read/write failed |

## `optix journal status`

Offline-safe — never contacts IBKR. Returns:

```json
{
  "last_sync_at": "2026-05-16T08:00:00Z",
  "hours_since_sync": 6.5,
  "gap_warning": false,
  "total_executions": 247,
  "earliest_record": "2024-08-12T13:30:00Z",
  "latest_record":   "2026-05-15T19:58:42Z"
}
```

`last_sync_at == ""` means "never synced" → `gap_warning` is always `true`.
`earliest_record`, `latest_record`, and `last_error` are omitted when empty
(`last_error` only appears when the most recent sync failed).

## `optix journal sync` / `POST /api/journal/sync`

```json
{
  "new_count": 3,
  "total_count": 247,
  "last_sync_at": "2026-05-16T14:23:01Z",
  "hours_since_sync": 0.0,
  "gap_warning": false,
  "ibkr_ok": true,
  "error": ""
}
```

HTTP: 200 on success, 502 with `ibkr_ok=false` + `error` populated on broker failure.
CLI exits 2 on broker failure.

## `optix journal list` / `GET /api/journal`

Query params (CLI flags): `symbol`, `side` (BOT/SLD), `type` (stk/opt), `since` (YYYY-MM-DD), `until`, `limit`.

```json
{
  "executions": [
    {
      "ExecID": "0001f4e8.6...",
      "Time": "2026-05-15T19:58:42Z",
      "Account": "DU1234567",
      "Symbol": "AAPL",
      "SecType": "STK",
      "Expiration": "",
      "Strike": 0,
      "Right": "",
      "Side": "BOT",
      "Shares": 100,
      "Price": 192.34,
      "AvgPrice": 192.34,
      "Currency": "USD",
      "Exchange": "SMART",
      "OrderID": 42,
      "PermID": 7
    }
  ]
}
```

Note: `Execution` fields use Go's default JSON tag style (PascalCase) because
the existing `pkg/model/account.go` type has no explicit JSON tags. If/when
JSON tags are added to `model.Execution`, the keys will switch to snake_case —
treat that as a versioned breaking change.

Ordered by `Time` DESC.

## `optix journal trips` / `GET /api/journal/trips`

Query params: `symbol`, `status` (open / closed / expired), `since`, `until`.

```json
{
  "round_trips": [
    {
      "symbol": "AAPL",
      "sec_type": "STK",
      "account": "DU1234567",
      "currency": "USD",
      "direction": "LONG",
      "open_time":  "2026-05-10T14:30:00Z",
      "close_time": "2026-05-14T19:00:00Z",
      "open_qty": 100,
      "close_qty": 100,
      "open_avg_price": 190.00,
      "close_avg_price": 195.00,
      "multiplier": 1,
      "realized_pnl": 500.0,
      "holding_days": 4.2,
      "status": "closed",
      "open_exec_ids":  ["E1"],
      "close_exec_ids": ["E2"]
    }
  ]
}
```

Ordered by `open_time` DESC. `close_time` is omitted (or zero) when `status == "open"`.
Option-only fields (`expiration`, `strike`, `right`) are omitted for stock round trips.

## `optix journal review` / `GET /api/journal/review`

Query params: `since`, `until`.

```json
{
  "since": "2026-05-01T00:00:00Z",
  "until": "2026-05-31T23:59:59Z",
  "total_executions": 47,
  "total_round_trips": 12,
  "closed_round_trips": 10,
  "open_round_trips": 2,
  "expired_round_trips": 0,
  "excluded_non_usd_round_trips": 1,
  "win_count": 7,
  "loss_count": 3,
  "win_rate": 0.7,
  "total_realized_pnl": 1234.50,
  "avg_realized_pnl": 123.45,
  "avg_holding_days": 3.2,
  "by_symbol": [
    {
      "symbol": "AAPL",
      "sec_type": "STK",
      "currency": "USD",
      "round_trip_count": 4,
      "realized_pnl": 500.00,
      "win_rate": 0.75
    }
  ]
}
```

`by_symbol` is sorted by `realized_pnl` DESC.

Review P&L metrics are USD-only. Non-USD round trips are omitted from realized
P&L, win/loss, holding-day, and by-symbol summary metrics until FX conversion is
implemented; their count is reported in `excluded_non_usd_round_trips`.

Aggregate `win_rate` uses `closed_round_trips + expired_round_trips` as denominator
(open trips don't count for or against win rate). Per-symbol `win_rate` uses the
same denominator scoped to that symbol.
