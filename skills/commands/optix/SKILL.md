---
name: optix
description: "Use when the user asks about US stock quotes, option chains, single-contract option quote validation, options strategies, Max Pain, watchlists, Optix dashboard summaries, IBKR positions, executions, trade journal review, portfolio concentration, Greeks, stress tests, risk exposure, market pulse, premarket/postclose/event-day/shock views, FOMC/CPI event analysis, macro-event sensitivity, regime triggers, shock fingerprints, or market liquidity state. Also use for Chinese requests about 美股行情、期权分析、单合约期权报价校验、自选股、持仓、成交记录、复盘、风险敞口、市场快照、盘前看盘、收盘后复盘、FOMC/CPI 事件日、突发冲击、流动性状态、隔夜行情。"
---

# Optix — 美股期权分析 / US Stock & Options Analysis

> ⚠️ **Read-only scope** / **只读范围**
>
> This skill exclusively reads market data, account holdings, and execution
> history from IBKR. It **cannot place, modify, or cancel orders** (no market
> orders, no limit orders, no options orders, no order replacement). All
> trading operations must be performed by the user directly in the IBKR
> client (TWS or Gateway).
>
> 本 skill 仅支持通过 IBKR **读取**行情、账户持仓与成交记录，**不支持**下单、
> 挂单、撤单等任何交易操作。下单、挂单请用户自行在 IBKR 客户端（TWS / Gateway）
> 中操作。

Use this skill when the user asks about (当用户提到以下内容时触发):
- 股价、行情、报价 / Stock prices, quotes (e.g., "AAPL 现在多少钱?", "查一下特斯拉股价", "get me a quote for TSLA")
- 期权分析、策略推荐 / Options analysis, strategy recommendations (e.g., "分析一下 NVDA", "有什么期权机会?", "analyze AAPL")
- 自选股、关注列表 / Watchlist management (e.g., "把 META 加入自选", "看看自选股", "删掉 COIN", "add to watchlist")
- 看板、总览 / Dashboard, overview (e.g., "看看大盘", "打开看板", "show dashboard", "how are my stocks doing?")
- 账户持仓、P&L、市值 / Account positions, holdings, P&L (e.g., "看看我的持仓", "我现在持有什么", "show my positions", "what do I hold?", "盈亏怎么样")
- 交易记录、近期成交 / Recent executions, trade history (e.g., "最近的交易", "近 7 天成交记录", "show recent trades", "trade history")
- 交易日记、复盘、长期成交记录 / Trade journal, retrospective, long-term execution history (e.g., "复盘最近一周的交易", "我这个月的胜率", "show my journal", "trade retrospective")
- Max Pain、指定到期日 / Max Pain for a specific expiration (e.g., "GOOGL 5/22 的 max pain 是多少?", "max pain for AAPL this Friday", "本周五的 max pain", "用 yfinance 算 max pain")
- 市场快照、盘前看盘、隔夜行情、多资产总览 / Market pulse, premarket overview, multi-asset snapshot (e.g., "现在大盘怎么样?", "盘前看盘", "隔夜期货", "market pulse", "premarket snapshot", "overnight futures")
- 隔夜传导、跳空回补、盘前异动、情绪定位 / Overnight chain, gap fill, premarket movers, sentiment positioning
- 事件日看盘、FOMC/CPI、利率路径、声明 diff、敏感度矩阵 / Event-day view, FOMC/CPI, rate-path repricing, statement diff, sensitivity matrix
- 突发冲击、regime 触发、冲击指纹、流动性状态 / Shock view, regime trigger, shock fingerprints, liquidity state

## Commands

Replace `<SYMBOL>` with the stock ticker the user mentions.

All commands invoke `bin/optix.sh`, a thin entry-point bundled with the skill.
The wrapper resolves the runtime (Go binary + Python engine) in this order:
1. `$OPTIX_HOME` environment variable (developer override; points to a source checkout)
2. `<skill_dir>/.runtime/` (release-mode install; populated by install.sh)
3. `optix` command on `$PATH` (system install, e.g. via Homebrew)

### Get stock quote
```bash
bash bin/optix.sh quote <SYMBOL>
bash bin/optix.sh quote <SYMBOL> --format json
```

### Analyze a stock (technicals + options + strategy recommendations)
```bash
bash bin/optix.sh analyze <SYMBOL>
bash bin/optix.sh analyze <SYMBOL> --weeks 4 --capital 100000 --risk conservative
bash bin/optix.sh analyze --watchlist --capital 100000
```

### Analyze with per-contract Open Interest (enables Max Pain)
```bash
bash bin/optix.sh analyze <SYMBOL> --with-oi
bash bin/optix.sh analyze <SYMBOL> --with-oi --expiry 2026-05-22   # specific expiration
```

The Max Pain line always shows the expiry used (e.g. `(expiry 2026-05-20)`), so the default-nearest pick is transparent. Use `--expiry YYYY-MM-DD` to override; bad expiries print a closest-first suggestion list.

### Show option chain
```bash
bash bin/optix.sh chain <SYMBOL>
bash bin/optix.sh chain <SYMBOL> --expiry 2026-05-22
bash bin/optix.sh chain <SYMBOL> --expiry 2026-05-22 --format json
```

### Validate one option contract quote with IBKR
```bash
bash bin/optix.sh option-quote AAPL --expiry 2026-07-17 --right P --strike 290 --format json
```

This is IBKR-only and intended for final candidate validation after free-source
option-chain screening. JSON includes bid, ask, mid, mark, last, volume,
open_interest, implied_volatility, Greeks when IBKR supplies them, source,
timestamp, market_data_type, and warnings. If IBKR returns no usable option
price data, the command still emits structured JSON warnings and exits non-zero.

### Show dashboard (all watchlist stocks with analysis)
```bash
bash bin/optix.sh dashboard
bash bin/optix.sh dashboard --sort iv-rank --top 5 --capital 100000
bash bin/optix.sh dashboard --format json
```

### List watchlist
```bash
bash bin/optix.sh watch list
```

### Add to watchlist
```bash
bash bin/optix.sh watch add <SYMBOL>
bash bin/optix.sh watch add AAPL MSFT NVDA
```

### Remove from watchlist
```bash
bash bin/optix.sh watch remove <SYMBOL>
```

### Show current account positions (stocks + options, with P&L)
```bash
bash bin/optix.sh positions
bash bin/optix.sh positions --type stk    # only stocks
bash bin/optix.sh positions --type opt    # only options
bash bin/optix.sh positions --format json
```

### Show recent executions (last 7 days)
```bash
bash bin/optix.sh trades
bash bin/optix.sh trades --symbol <SYMBOL>     # filter by symbol
bash bin/optix.sh trades --side BOT            # only buys (or SLD for sells)
bash bin/optix.sh trades --since 2026-05-10    # only on/after this date
bash bin/optix.sh trades --format json
```

### Trade Journal (交易日记 / 复盘)

Persists IBKR executions to a local SQLite database, working around IBKR's
7-day history limit. All journal commands support `--format json` for
agent-friendly structured output.

Decision flow for agents:
1. Use `journal status --format json` first when the user asks for retrospective
   analysis or older trade history.
2. If the journal is stale and IBKR is available, run `journal sync --format json`.
3. Use `journal review` for summary metrics, `journal trips` for round-trip P&L,
   and `journal list --no-sync` when IBKR is unavailable but SQLite already has data.

#### Journal status (does NOT require IBKR)
```bash
bash bin/optix.sh journal status --format json
```

#### Pull recent executions into the journal
```bash
bash bin/optix.sh journal sync --format json
```

#### List persisted executions
```bash
bash bin/optix.sh journal list --symbol AAPL --since 2026-05-01 --format json
bash bin/optix.sh journal list --type opt --side SLD --limit 50 --no-sync --format json
```

#### View round-trip P&L
```bash
bash bin/optix.sh journal trips --status closed --format json
bash bin/optix.sh journal trips --symbol AAPL --since 2026-05-01 --until 2026-05-31 --no-sync --format json
```

#### Retrospective summary
```bash
bash bin/optix.sh journal review --since 2026-05-01 --format json
bash bin/optix.sh journal review --since 2026-05-01 --until 2026-05-31 --no-sync --format json
```

Pass `--no-sync` to `journal list`, `journal trips`, or `journal review` to
skip the best-effort IBKR round-trip and read SQLite only (useful when IBKR is
unavailable or after a recent sync). `journal status` is always offline-safe
and does not have a `--no-sync` flag.

Exit codes: `0` success · `1` generic error · `2` IBKR unreachable · `3` SQLite error.

### Max Pain (specific expiration)

Standalone Max Pain query for one option expiration. Reuses the existing
`GetMaxPain` gRPC RPC; supports IBKR, Yahoo Finance, or automatic fallback.

```bash
bash bin/optix.sh max-pain GOOGL --expiry 2026-05-22 --format json
bash bin/optix.sh max-pain GOOGL --source yfinance --format json   # no IBKR required
bash bin/optix.sh max-pain GOOGL --expiry 2026-05-22               # text mode
```

`--source ibkr|yfinance|auto` (default `auto` — IBKR first, fall back to
Yahoo Finance). `--expiry YYYY-MM-DD` defaults to the broker's nearest
expiration. Bad expiries print a closest-first suggestion list (in JSON
mode: structured `expiry_not_available` envelope with sorted `available`
and `suggestion` fields).

JSON includes a `max_pain_offset_pct` field = `(max_pain - spot) / spot × 100`
— positive = Max Pain above spot, negative = below. Useful for agents to
decide directional bias at a glance.

Exit codes: `0` success · `1` generic error or bad flags · `2` broker
unreachable · `3` SQLite error.

### Market Pulse (市场快照 / 盘前看盘)

Multi-asset market snapshot: indices, futures proxies, FX, yields, vol family.
**No IBKR and no Python gRPC engine required** — uses free delayed sources
(the skill's bundled venv already includes yfinance).

```bash
bash bin/optix.sh pulse --format json
bash bin/optix.sh pulse --view premarket --format json
bash bin/optix.sh pulse --format json --with-sparkline
```

View defaults to clock inference (premarket/intraday/postclose, America/New_York);
off-hours (隔夜/周末/NYSE 假日) map to `postclose` — last session's frozen
snapshot (built-in NYSE 2026–2027 calendar). `event`/`shock` views via explicit
`--view`. Each asset carries a `basis` label (delayed/approx/frozen); failed
assets appear in `missing[]` without failing the command. Exit codes: 0 success
(even with warnings) · 1 with --strict when no data · 3 SQLite error.

The same phase clock + pulse contract powers the embedded Market Intel cockpit:
launch `optix-server` and open `http://127.0.0.1:8080/intel/` in a browser for
the Pulse bar + phase-following view: the intraday movers/sector-heatmap cards
are real IBKR-preferred data (#184), not a placeholder, alongside the M4–M7
premarket/postclose/event/shock cards. It ships inside the server binary — no
separate build needed.

### Premarket View (盘前四卡 / M4)

Four pure-compute premarket cards: overnight transmission chain, implied open
plus historical SPX gap-fill statistics, premarket movers plus volume ratio, and
sentiment positioning (Put/Call + VIX term premium). **No IBKR and no Python
gRPC engine required** — uses yfinance subprocesses and local SQLite for cached
gap-fill distributions.

```bash
bash bin/optix.sh premarket
bash bin/optix.sh premarket --format json
```

Agents should read `premarket --format json` at the 08:00 剧本 checkpoint before
writing the playbook narrative. It complements `pulse --format json`: `pulse`
gives the multi-asset board; `premarket` gives the four-card premarket read.

### Postclose View (收盘后四卡 / M5)

Four pure-compute postclose cards: earnings quick read vs free yfinance EPS
consensus, structured postclose timeline, same-sector read-across edges, and
combined regular-session plus after-hours movers. **No IBKR and no Python gRPC
engine required** — uses yfinance subprocesses plus the embedded sector map.

```bash
bash bin/optix.sh postclose
bash bin/optix.sh postclose --format json
```

Agents should read `postclose --format json` at the 16:30 对账 / 收盘后
checkpoint. It complements `intel reconcile`: `postclose` supplies structured
market facts; the agent writes narrative and judgments through `optix intel`
(`narrative` / `judge`), reads them with `intel read`, and settles them with
`intel reconcile`.

### Event View (事件日四卡 / M6)

Four pure-compute event-day cards: rate/yield proxy repricing, deterministic
FOMC statement wording diff, historical FOMC/CPI event-day patterns, and a
signed cross-asset sensitivity matrix. **No IBKR and no Python gRPC engine
required** — M6 uses yfinance for market rows plus Fed.gov FOMC
calendar/statement and BLS CPI schedule fetchers with local deterministic
fallback. Each row carries explicit source/basis/as_of and warnings when
degraded; it is not trading-grade live Fed Funds futures pricing.

```bash
bash bin/optix.sh event
bash bin/optix.sh event --format json
```

Agents should read `event --format json` around scheduled FOMC/CPI events.
It complements `pulse --view event --format json`: `pulse` gives the current
multi-asset board; `event` gives the four-card event interpretation layer.

### Shock View (突发冲击四卡 / M7)

Four pure-compute shock cards: regime trigger scoring, supply/demand/liquidity/
policy shock fingerprints, local historical analog matching, and ETF liquidity
state. M7 is **IBKR-preferred by contract** for realtime L1, bid/ask spread,
market depth, tick-by-tick, and option IV/Greeks/OI/volume. v1 already overlays
broker-backed ETF top-of-book bid/ask data when IBKR is available, and remains
runnable without IBKR through yfinance quote fallback. Follow-up patches add an
IBKR SMART market-depth adapter for core ETFs and option-stress metrics from
broker/yfinance option chains; missing subscriptions or source failures still
degrade explicitly with warnings.

```bash
bash bin/optix.sh shock
bash bin/optix.sh shock --format json
```

Agents should read `shock --format json` during abnormal cross-asset moves.
It complements `pulse --view shock --format json`: `pulse` gives the current
multi-asset board; `shock` gives the four-card trigger/fingerprint/liquidity
interpretation layer.

### Market Intel — 判断日记检查点工作流 (judgment journal)

A closed judgment loop: at daily checkpoints an agent writes narrative prose and
registers **falsifiable directional judgments**; optix captures the registration
price, settles expired judgments against price history, and tracks hit-rate.
**No IBKR and no Python gRPC engine required** — `judge` uses the skill venv's
yfinance to capture the registration price; everything else is pure SQLite.

Triggers: 写剧本/复盘/对账、登记判断、检查点叙事、命中率, "write the playbook",
"register a judgment", "reconcile", "checkpoint narrative", "hit rate".

**Four daily checkpoints (America/New_York), plus ad-hoc `interrupt`:**

| Checkpoint     | ET    | 含义 | Action |
| -------------- | ----- | ---- | ------ |
| `script`       | 08:00 | 剧本 | premarket playbook narrative |
| `first_check`  | 10:30 | 首验 | first read of the open; register judgments |
| `set_tone`     | 15:00 | 定调 | late-session tone; register/supersede judgments |
| `reconcile`    | 16:30 | 对账 | settlement step — run `intel reconcile` |
| `interrupt`    | ad-hoc | 突发 | unscheduled narrative on a shock |

**Fill-slot flow at each checkpoint:**

```bash
# 1. orient: where are we in the day + today's hit-rate so far
bash bin/optix.sh intel status --format json

# 2. read the market (zero IBKR/gRPC; delayed sources)
bash bin/optix.sh pulse --format json
bash bin/optix.sh premarket --format json

# 3. write the checkpoint narrative (append-only; body truncated to 8KB)
bash bin/optix.sh intel narrative --kind script --body "..." --format json

# 4. register a falsifiable judgment — optix captures the price
bash bin/optix.sh intel judge --asset SPX --direction up --threshold 0.5 \
  --confidence 75 --expiry reconcile --rationale "..." --format json

# 5. at 16:30 (对账): settle every expired judgment against price history
bash bin/optix.sh intel reconcile --format json

# review a trading day's full timeline (narratives + judgments + reconciliation)
bash bin/optix.sh intel read --date 2026-06-12 --format json
```

**Judgment discipline (the third pillar — guards against unfalsifiable claims):**

- `judge` requires a **structured, falsifiable assertion**: an asset
  (`--asset`, must be a registered AssetRef like SPX/ES/VIX), a `--direction`
  (`up|down|flat`), a `--threshold` %, a `--confidence` (0–100), and an
  `--expiry` checkpoint (`first_check|set_tone|reconcile`) that is **later than
  now** on the same trading day. Subjective color ("情绪谨慎") belongs in the
  narrative, never in a judgment.
- optix **captures the registration price** at `judge` time and **owns the
  verdict** at `reconcile`; the agent never supplies either. If the asset price
  cannot be captured, the judgment is **rejected and not written** (exit code 1)
  — never register a judgment that cannot be settled.
- To revise a call, register a new judgment with `--supersedes <judgment_id>`.
  Supersede **does not erase** the original — both stay in the append-only
  record; the superseded judgment is greyed in the panel and still counts toward
  the audit trail.
- M3 settles **same-day** expiries only; a judgment registered at `reconcile`
  has no later checkpoint to expire into and is rejected (16:30 is the
  settlement step, not a registration step).

The stored journal renders read-only at `http://127.0.0.1:8080/intel/`
(intraday 叙事流 panel) via `GET /api/intel/journal`. Writes go only through the
CLI; the server reads; cross-process concurrency uses SQLite WAL.

### Portfolio Risk (持仓风险 / 组合 Greeks)

Account-level risk views across all holdings. **Requires IBKR** (no Yahoo Finance fallback).

#### Concentration (集中度：单名/板块权重 + 阈值旗标)
```bash
bash bin/optix.sh portfolio concentration --net-liq-usd <NLV>
bash bin/optix.sh portfolio concentration --net-liq-usd <NLV> --json /tmp/conc.json
bash bin/optix.sh portfolio concentration --net-liq-usd <NLV> --json -
bash bin/optix.sh portfolio concentration --net-liq-usd <NLV> --net-liq-sgd <NLV_SGD> --fx-usd-sgd <FX>
bash bin/optix.sh portfolio concentration --threshold-warn 8 --threshold-red 18 --top-n 15
```
Per-underlying and per-sector weights, Top-N, HHI diversification bucket, and threshold flags (default 10% yellow, 20% red). Non-USD holdings and closed-out residuals are excluded with a warning. `--sectors-file` overrides the ticker-to-sector map; `--portfolio-config` overrides the risk YAML config.

#### Greeks (组合 Δ/Γ/Vega/Θ 聚合)
```bash
bash bin/optix.sh portfolio greeks --by underlying --net-liq-usd <NLV>
bash bin/optix.sh portfolio greeks --by sector --json /tmp/greeks.json
bash bin/optix.sh portfolio greeks --by sector --json -
bash bin/optix.sh portfolio greeks --risk-free-rate 0.043 --sectors-file configs/sectors.json
```
Net Δ = delta-adjusted shares; Dollar Δ = USD exposure per +1% spot; Vega(/1%) and Θ/day in USD. IV comes from the option chain (falls back to inverting the mark). Legs with no resolvable IV are skipped and listed. Needs the Python analysis engine (auto-started by the skill). `--portfolio-config` can supply the default risk-free rate and sector map.

#### Stress test (场景压力测试)
```bash
bash bin/optix.sh portfolio stress --net-liq-usd <NLV>
bash bin/optix.sh portfolio stress --portfolio-config configs/portfolio.yaml --json /tmp/stress.json
bash bin/optix.sh portfolio stress --portfolio-config configs/portfolio.yaml --json -
bash bin/optix.sh portfolio stress --risk-free-rate 0.043 --sectors-file configs/sectors.json
```
Scenario P&L using the same Greeks snapshot as `portfolio greeks`. Default scenarios are config-driven: SPY -3/-5/-10%, IV +5 points, QQQ -5%, and a tech-correlated SPY/IV shock. Text output shows total P&L, % NLV, and worst position per scenario; JSON is stable for cron/agent consumers.

## Notes
- Python gRPC analysis engine auto-starts/stops on port 50053.
- Prefer `--format json` or `--json -` when another agent/tool needs to parse the result. Diagnostic messages are written to stderr in JSON mode.
- IBKR TWS/Gateway is **optional** for quote / analyze / dashboard / chain — they fall back to Yahoo Finance delayed data if IBKR is unreachable
- **`positions`, `trades`, and `option-quote` REQUIRE IBKR** — account data and single-contract quotes have no Yahoo Finance fallback; the commands print a clear error and exit non-zero when TWS/Gateway is not running
- Override the IBKR connection with `OPTIX_IB_HOST` and `OPTIX_IB_PORT` (for example `OPTIX_IB_PORT=4002` for paper Gateway, `7497` for paper TWS, `4001` for live Gateway, or `7496` for live TWS)
- `trades` only covers the last ~7 days (IBKR's `ReqExecutions` window); `--since` older than 7 days is clamped with a warning
- `positions` option mark prices require an OPRA market-data subscription; without it the option Mark / MktValue / UnrealPnL columns degrade to `—` (identity + cost columns still render)
- Connection pool (8 slots, ClientIDs 30–37) is managed automatically; TWS restart is handled gracefully
- `analyze --with-oi` uses IBKR market-data ticks for Open Interest and requires a matching subscription (e.g. OPRA Top of Book); `max-pain --source yfinance` can use delayed Yahoo Finance OI without IBKR
- `journal status` is offline-safe — does not require IBKR; useful for agents to decide whether to call `journal sync` first
- `journal list`, `journal trips`, and `journal review` auto-sync best-effort before reading SQLite unless `--no-sync` is passed
- `journal sync` requires IBKR; use it before review/list/trips when the user wants the latest executions captured in SQLite
- `max-pain --source yfinance` works without IBKR; yfinance returns Open Interest inline, so no OPRA subscription is needed (delayed quotes, may differ slightly from IBKR's real-time chain)
- `analyze --with-oi` and `max-pain` both surface a closest-first suggestion list when `--expiry` doesn't match — agents/users can copy-paste the "Did you mean" line
