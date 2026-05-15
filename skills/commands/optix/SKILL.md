---
name: optix
description: "美股期权分析工具 / US stock & options analysis: 查看股价行情、期权分析、策略推荐、自选股管理、看板总览、IBKR 账户持仓与交易记录。Use when user asks about stock prices, quotes, options strategies, market analysis, watchlist, dashboard, IBKR positions, or trade history."
---

# Optix — 美股期权分析 / US Stock & Options Analysis

Use this skill when the user asks about (当用户提到以下内容时触发):
- 股价、行情、报价 / Stock prices, quotes (e.g., "AAPL 现在多少钱?", "查一下特斯拉股价", "get me a quote for TSLA")
- 期权分析、策略推荐 / Options analysis, strategy recommendations (e.g., "分析一下 NVDA", "有什么期权机会?", "analyze AAPL")
- 自选股、关注列表 / Watchlist management (e.g., "把 META 加入自选", "看看自选股", "删掉 COIN", "add to watchlist")
- 看板、总览 / Dashboard, overview (e.g., "看看大盘", "打开看板", "show dashboard", "how are my stocks doing?")
- 账户持仓、P&L、市值 / Account positions, holdings, P&L (e.g., "看看我的持仓", "我现在持有什么", "show my positions", "what do I hold?", "盈亏怎么样")
- 交易记录、近期成交 / Recent executions, trade history (e.g., "最近的交易", "近 7 天成交记录", "show recent trades", "trade history")

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
```

### Analyze a stock (technicals + options + strategy recommendations)
```bash
bash bin/optix.sh analyze <SYMBOL>
```

### Analyze with per-contract Open Interest (enables Max Pain)
```bash
bash bin/optix.sh analyze <SYMBOL> --with-oi
```

### Show dashboard (all watchlist stocks with analysis)
```bash
bash bin/optix.sh dashboard
```

### List watchlist
```bash
bash bin/optix.sh watch list
```

### Add to watchlist
```bash
bash bin/optix.sh watch add <SYMBOL>
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
```

### Show recent executions (last 7 days)
```bash
bash bin/optix.sh trades
bash bin/optix.sh trades --symbol <SYMBOL>     # filter by symbol
bash bin/optix.sh trades --side BOT            # only buys (or SLD for sells)
bash bin/optix.sh trades --since 2026-05-10    # only on/after this date
```

## Notes
- Python gRPC server auto-starts/stops on port 50053 (separate from local dev server on 50052)
- IBKR TWS/Gateway is **optional** for quote / analyze / dashboard / chain — they fall back to Yahoo Finance (delayed quotes, no options chain) if IBKR is unreachable
- **`positions` and `trades` REQUIRE IBKR** — account data has no Yahoo Finance fallback; the commands print a clear error and exit non-zero when TWS/Gateway is not running
- `trades` only covers the last ~7 days (IBKR's `ReqExecutions` window); `--since` older than 7 days is clamped with a warning
- `positions` option mark prices require an OPRA market-data subscription; without it the option Mark / MktValue / UnrealPnL columns degrade to `—` (identity + cost columns still render)
- Connection pool (8 slots, ClientIDs 30–37) is managed automatically; TWS restart is handled gracefully
- `--with-oi` requires an IBKR market data subscription (e.g. OPRA Top of Book) for Open Interest ticks
