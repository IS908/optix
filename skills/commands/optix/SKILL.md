---
name: optix
description: "美股期权分析工具 / US stock & options analysis: 查看股价行情、期权分析、策略推荐、自选股管理、看板总览。Use when user asks about stock prices, quotes, options strategies, market analysis, watchlist, or dashboard."
---

# Optix — 美股期权分析 / US Stock & Options Analysis

Use this skill when the user asks about (当用户提到以下内容时触发):
- 股价、行情、报价 / Stock prices, quotes (e.g., "AAPL 现在多少钱?", "查一下特斯拉股价", "get me a quote for TSLA")
- 期权分析、策略推荐 / Options analysis, strategy recommendations (e.g., "分析一下 NVDA", "有什么期权机会?", "analyze AAPL")
- 自选股、关注列表 / Watchlist management (e.g., "把 META 加入自选", "看看自选股", "删掉 COIN", "add to watchlist")
- 看板、总览、持仓概览 / Dashboard, overview (e.g., "看看大盘", "打开看板", "show dashboard", "how are my stocks doing?")

## Commands

Replace `<SYMBOL>` with the stock ticker the user mentions.

### Get stock quote
```bash
bash skills/commands/optix/optix.sh quote <SYMBOL>
```

### Analyze a stock (technicals + options + strategy recommendations)
```bash
bash skills/commands/optix/optix.sh analyze <SYMBOL>
```

### Show dashboard (all watchlist stocks with analysis)
```bash
bash skills/commands/optix/optix.sh dashboard
```

### List watchlist
```bash
bash skills/commands/optix/optix.sh watch list
```

### Add to watchlist
```bash
bash skills/commands/optix/optix.sh watch add <SYMBOL>
```

### Remove from watchlist
```bash
bash skills/commands/optix/optix.sh watch remove <SYMBOL>
```

## Notes
- Python gRPC server auto-starts/stops for `analyze` and `dashboard` — no manual setup needed
- Uses port 50053 by default (separate from local dev server on 50052)
- Requires IBKR TWS/Gateway running for live market data
