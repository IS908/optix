# Market Intel M7 — 突发冲击视图 Design Spec

> Status: M7 kickoff approved after IBKR data-source review.
> Preceded by M1 v0.8.0 through M6 v0.13.0.
> Branch `codex/m7-shock` off master -> target **v0.14.0**.
> Sub-issue: #134.

## 1. Goal

Replace the four Shock view placeholder slots with real, source-backed
components for fast market-stress diagnostics: regime trigger, shock
fingerprint, historical analogs, and liquidity state. M7 also exposes a pure
mechanism-trigger decision DTO that later scheduler code can use to override
the normal phase view into `shock`.

Optix remains read-only and pure-compute in M7 v1: no order entry, no LLM calls,
no long-running background scanner, and no hard dependency on IBKR. When IBKR is
available, shock metrics prefer it for realtime and microstructure data. When it
is unavailable, the same contracts degrade to yfinance/local fixtures with clear
`source`, `basis`, `as_of`, and `warnings` labels.

## 2. Data Source Decision

M7 uses an **IBKR-preferred, free-source fallback** architecture.

### Prefer IBKR

Use an IBKR-backed adapter when available for:

1. **Realtime L1 quotes** for SPY, QQQ, IWM, TLT, HYG, LQD, GLD, USO, UUP,
   VIXY, ES, NQ, RTY, YM, CL, GC, ZN, and ZB. These feed regime triggers and
   cross-asset confirmation.
2. **Bid/ask and spread** for ETFs and futures. These feed liquidity state and
   help distinguish real stress from stale last-price moves.
3. **ETF Level II / market depth** for SPY, QQQ, IWM, TLT, HYG, and LQD.
   M7 v1 stores the interface and DTO shape; market-depth rows explicitly
   degrade until a broker depth adapter is added.
4. **Tick-by-tick bid/ask and last** for a small core set such as SPY, QQQ,
   TLT, HYG, and VIXY. This is a future adapter path, not required for M7 v1.
5. **Option IV, Greeks, OI, and volume** for SPX/SPY, QQQ, IWM, and VIX/VIXY.
   M7 v1 exposes vol-repricing fields that can use yfinance option-chain
   fallbacks first; IBKR becomes the preferred source when OPRA subscriptions
   are present.
6. **Account positions and exposure** only when a future portfolio-shock overlay
   is added. Free sources cannot supply account data.

### Keep Free/Local

Use yfinance, local fixtures, or existing caches for:

1. Multi-year historical analog libraries and daily return vectors.
2. Public macro context and policy-date labels.
3. Historical VIX/asset distributions used for percentile thresholds.
4. Text/news narrative. Agent-written narrative remains in the M3 journal path,
   not inside M7 compute.

## 3. Components

### Regime Trigger

Compute a trigger state from:

- VIX move in sigma units versus local/historical distribution.
- Cross-asset confirmation across equities, rates, credit, dollar, oil, gold,
  and volatility proxies.
- Optional liquidity confirmation from spread/depth stress.

Output includes:

- `state`: `normal`, `watch`, `shock`, or `critical`.
- `score`: 0-100.
- `vix_sigma`.
- `confirmations[]`: signed contributors with source/basis/as_of.
- `triggered_view`: `shock` only when state is `shock` or `critical`.

The score is a deterministic heuristic, not a trained regime model.

### Shock Fingerprint

Classify the current shock vector into four interpretable dimensions:

- `supply`: oil up, rates up, dollar up, gold mixed/up.
- `demand`: equities down, yields down, oil down, credit weak.
- `liquidity`: spreads wider, VIX up, credit ETFs weak, depth thin.
- `policy`: rates/yields/dollar up with equities weak or mixed.

Each fingerprint row includes score, confidence, primary evidence, and missing
data notes. Multiple fingerprints can be active at once; M7 does not force a
single label when evidence is mixed.

### Historical Analogs

Compare the current shock vector to a small local library of named historical
shock templates. Each analog row includes:

- `name`, `date`, and `category`.
- `similarity` in 0-1.
- `next_session_bias`: `risk_on`, `risk_off`, or `mixed`.
- `matched_features[]`.

The first version uses curated templates rather than a database migration. This
keeps M7 testable and deterministic. A later backfill job can replace the local
library without changing the DTO shape.

### Liquidity State

Compute a market-liquidity dashboard from:

- ETF bid/ask spread in bps.
- Spread z-score versus local fallback thresholds.
- Top-of-book depth and top-5 depth when IBKR depth is available.
- Missing-depth warnings when only free delayed quotes are available.

Rows cover SPY, QQQ, IWM, TLT, HYG, and LQD. When IBKR is not available, the
card still renders spread estimates from available quote fields and marks depth
as missing instead of failing the request.

## 4. Source Interface

Add `internal/shockintel` with a small source interface:

```go
type Source interface {
    Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error)
    Bars(ctx context.Context, ids []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error)
    Depth(ctx context.Context, ids []string, levels int) (map[string]DepthSnapshot, error)
    OptionMetrics(ctx context.Context, underlyings []string) (map[string]OptionStress, error)
}
```

`YFinanceAdapter` implements quotes/bars and returns explicit warnings for
depth and exact option stress. `BrokerQuoteAdapter` is wired in M7 v1 as the
IBKR-preferred path for ETF top-of-book quote/bid/ask data, with yfinance used
for macro/index sensors and as the broker fallback. Market depth and exact
option stress remain explicit degraded paths.

## 5. CLI and HTTP

CLI:

```bash
optix shock --format text|json
```

HTTP:

```text
GET /api/intel/shock/regime
GET /api/intel/shock/fingerprint
GET /api/intel/shock/analogs
GET /api/intel/shock/liquidity
```

Each endpoint returns HTTP 200 with warnings when a source is degraded. A nil
shock service returns 503, matching M4-M6 behavior.

## 6. SPA

Extend `SlotDef.live` with:

- `shock-regime`
- `shock-fingerprint`
- `shock-analogs`
- `shock-liquidity`

Replace the four M7 placeholders with dense operational cards:

- compact tables and score bars;
- visible source/basis/as-of labels;
- warnings rendered inline but not as blocking page errors;
- no nested cards and no explanatory marketing text.

## 7. Non-Goals

- Order placement or hedging recommendations.
- A live scanner that automatically subscribes to dozens of IBKR streams.
- News ingestion, LLM classification, or narrative generation.
- Persistent scheduler override storage.
- Full OPRA/CME/CBOE subscription management.
- Multi-year backfill jobs or a migration-backed analog database.
- Portfolio-level loss attribution in this slice.

## 8. Tests

- Pure Go tests for regime scoring, fingerprint classification, analog
  similarity, and liquidity state.
- Service tests for source degradation, IBKR-preferred metadata, and non-null
  slices.
- HTTP tests for nil 503 and happy-path shock endpoints.
- CLI tests for command registration and JSON bundle shape.
- Vitest tests for the four Shock cards.
- WebUI acceptance test wiring `/api/intel/shock/*` and `/intel/`.
- Verification before release: `go test ./... -count=1`, `go vet ./...`,
  `cd web && npm test`, `cd web && npm run build`, `make build`, and a WebUI
  screenshot of the Shock view.
