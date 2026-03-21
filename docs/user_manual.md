# Optix 使用手册

> 美股期权卖方策略分析系统
> 版本：Phase 6 (2026-03-21)
> 数据源：Interactive Brokers TWS / IB Gateway

---

## 目录

1. [系统架构概述](#1-系统架构概述)
2. [环境准备与启动](#2-环境准备与启动)
3. [Web UI 使用指南](#3-web-ui-使用指南)
4. [CLI 命令手册](#4-cli-命令手册)
5. [技术指标计算方法](#5-技术指标计算方法)
6. [波动率体系](#6-波动率体系)
7. [趋势评分算法](#7-趋势评分算法)
8. [期权分析指标](#8-期权分析指标)
9. [策略推荐决策逻辑](#9-策略推荐决策逻辑)
10. [Black-Scholes 定价与希腊值](#10-black-scholes-定价与希腊值)
11. [评分体系](#11-评分体系)
12. [关键阈值速查表](#12-关键阈值速查表)
13. [实战使用流程](#13-实战使用流程)
14. [已知局限性与注意事项](#14-已知局限性与注意事项)

---

## 1. 系统架构概述

```
┌──────────────────────────┐  ┌───────────────────────────────────┐
│  Web UI (浏览器)          │  │  CLI (Go)                         │
│  http://127.0.0.1:8080   │  │  optix quote / analyze / watch    │
│  Dashboard / Analyze /   │  │                                   │
│  Watchlist / Help        │  │                                   │
└────────────┬─────────────┘  └──────────────┬──────────────────┘
             │ HTTP + JSON API               │ 直接调用
             └───────────┬───────────────────┘
                         │
              ┌──────────▼───────────┐
              │  Go Backend Server   │
              │  数据采集 / 缓存 /    │
              │  后台调度 / API 服务   │
              └───┬──────────┬───────┘
                  │          │ gRPC
         ┌────────▼──┐  ┌───▼───────────────────┐
         │ IB TWS /  │  │ Python Analysis Engine │
         │ Gateway   │  │ localhost:50052        │
         │ port 7497 │  │ BS定价 / 技术分析 /    │
         └───────────┘  │ 期权分析 / 策略推荐    │
                        └───────────────────────┘
                                 │
                       ┌─────────▼──────────────┐
                       │  SQLite 数据库          │
                       │  ./data/optix.db        │
                       │  WAL 模式 / 并发读写    │
                       └────────────────────────┘
```

**组件说明：**

| 组件 | 技术 | 职责 |
|------|------|------|
| Web UI | HTML + Tailwind CSS + JS | 浏览器端交互界面、异步轮询、动态时间戳 |
| Go Backend | Go HTTP + gRPC | Web 服务器、IB 连接、数据采集、后台调度、API |
| CLI | Go (Cobra) | 命令行交互界面 |
| Analysis Engine | Python gRPC | 技术分析、波动率、策略推荐、BS 定价 |
| IB TWS/Gateway | Interactive Brokers | 实时行情、历史K线、期权链结构 |
| SQLite | 本地数据库 (WAL) | 自选股、行情缓存、OHLCV、期权链、分析结果、快照 |

---

## 2. 环境准备与启动

### 2.1 前置条件

1. **IB TWS 或 IB Gateway** 已启动并允许 API 连接
   - 纸盘：端口 `7497`（TWS）或 `4002`（Gateway）
   - 实盘：端口 `7496`（TWS）或 `4001`（Gateway）
   - 设置路径：`Edit → Global Configuration → API → Settings`
   - 勾选 `Enable ActiveX and Socket Clients`

2. **Python Analysis Engine** 已启动
   ```bash
   cd python
   .venv/bin/python -m optix_engine.grpc_server.server
   # 或
   python -m optix_engine.grpc_server.server
   ```

3. **配置文件**（可选，默认值已内置）
   ```bash
   cp configs/optix.yaml.example configs/optix.yaml
   ```

### 2.2 配置文件说明（configs/optix.yaml）

```yaml
ibkr:
  host: "127.0.0.1"
  port: 7497          # 纸盘TWS=7497, 实盘TWS=7496
  client_id: 1

grpc:
  python_server_addr: "localhost:50052"

database:
  path: "./data/optix.db"

analysis:
  forecast_days: 14           # 预测周期（天）
  risk_tolerance: "moderate"  # conservative / moderate / aggressive
  position_size_limit: 0.20   # 每笔仓位上限（占总资金比例）
```

### 2.3 编译与运行

```bash
# 编译所有二进制
make build
# 产出：bin/optix-cli, bin/optix-server

# 或直接运行（开发模式）
go run ./cmd/optix-cli/ [命令] [参数]
```

### 2.4 启动 Web UI（推荐方式）

```bash
# 终端1：启动 Python 分析引擎
make py-server

# 终端2：启动 Web 后端服务器（含后台调度器）
make run-server
# 或
./bin/optix-server --web-addr 127.0.0.1:8080

# 浏览器打开 http://127.0.0.1:8080
```

> **后台调度器**：Web 服务器启动后自动开始后台刷新任务，按周期采集 IB 行情、OHLCV 历史、期权链，并调用 Python 引擎执行分析。无需手动触发。

---

## 3. Web UI 使用指南

### 3.1 页面概览

| 页面 | 路径 | 功能 |
|------|------|------|
| Dashboard | `/` | 自选股概况表、机会评分、一键对比 |
| Analyze | `/analyze/{SYMBOL}` | 单只股票深度分析报告 |
| Watchlist | `/watchlist` | 添加/删除自选股 |
| Help | `/help` | 指标公式与阈值说明 |

### 3.2 Dashboard 页面

Dashboard 是系统的核心操作界面，提供：

- **概况表**：展示自选股的价格、趋势、RSI、IV Rank、Max Pain、PCR、价格区间、策略推荐、机会评分
- **`★` 星标**：机会评分 ≥ 50 的标的自动加星
- **数据新鲜度面板**（可折叠）：显示每个标的 5 个数据维度的最后更新时间
  - Quote（行情）、OHLCV（K线）、Options（期权链）、Analysis（分析缓存）、Snapshot（快照）
- **实时刷新按钮**：`?refresh=true` 触发从 IB 重新采集并分析

**异步轮询**：Dashboard 页面每 10 秒自动轮询后端 API，当后台调度器完成新一轮分析后，前端数据自动更新（含闪绿动画），无需手动刷新。

### 3.3 Analyze 详情页

点击 Dashboard 中的标的链接进入详情页，展示：

- **技术面分析**：趋势方向/评分、MA20/50/200、RSI、MACD、布林带、支撑阻力位
- **期权面分析**：IV Current / HV20、IV Rank / Percentile、Max Pain、PCR、期权墙
- **市场展望**：方向预判、置信度、1σ/2σ 价格区间
- **策略推荐 Top 3**：含行权价、权利金、盈亏比、胜率、保证金等
- **数据新鲜度条**：显示 Quote / OHLCV / Options / Analysis 四个时间戳

### 3.4 动态相对时间戳

所有页面的时间戳采用**动态相对显示**：

| 时间差 | 显示 |
|--------|------|
| < 1 分钟 | `just now` |
| 1–59 分钟 | `Xm ago` |
| 1–23 小时 | `Xh ago` |
| 1–6 天 | `Xd ago` |
| ≥ 7 天 | `YYYY-MM-DD` |

时间戳每 30 秒自动 tick 更新，页面隐藏时暂停以节省资源。

**颜色联动**：新鲜度徽章颜色随时间自动变化：
- 🟢 绿色：< 6 小时（数据新鲜）
- 🟡 琥珀色：6–48 小时（数据可能过时）
- 🔴 红色：> 48 小时（数据陈旧）
- ⚫ 灰色：从未获取

### 3.5 两阶段刷新模型

| 模式 | 触发方式 | 速度 | 数据来源 |
|------|---------|------|---------|
| 缓存模式（默认） | 直接访问页面 | 快 | SQLite 缓存 |
| 实时刷新 | `?refresh=true` | 慢 | IB TWS → Python 分析 → SQLite |
| 后台调度 | 自动（服务器启动后） | 异步 | IB TWS → Python 分析 → SQLite → 前端轮询 |

### 3.6 JSON API

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/dashboard` | GET | Dashboard 全量数据（含快照 + 新鲜度） |
| `/api/freshness` | GET | 所有标的的数据新鲜度时间戳 |
| `/api/watchlist/add` | POST | 添加自选股（JSON body: `{"symbols": [...]}`) |
| `/api/watchlist/remove` | POST | 删除自选股（JSON body: `{"symbol": "..."}`) |

---

## 4. CLI 命令手册

### 全局参数（所有命令均可用）

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--config` | `./configs/optix.yaml` | 配置文件路径 |
| `--db` | `./data/optix.db` | SQLite 数据库路径 |
| `--ib-host` | `127.0.0.1` | IB TWS/Gateway 地址 |
| `--ib-port` | `7497` | IB TWS/Gateway 端口 |

---

### 4.1 `optix quote [symbol]`

获取股票实时报价。

```bash
optix quote AAPL
optix quote COIN
```

**输出字段：**
- 最新价 / 买价 / 卖价
- 成交量 / 涨跌额 / 涨跌幅
- 时间戳

---

### 4.2 `optix watch`

管理自选股列表（存储于 SQLite）。

```bash
# 添加
optix watch add AAPL TSLA MSFT

# 批量添加高 IV 候选
optix watch add MSTR COIN HOOD GME

# 删除
optix watch remove AAPL

# 查看列表
optix watch list
```

---

### 4.3 `optix analyze [symbol]`

对单只股票或全部自选股进行深度分析，给出期权策略推荐。

```bash
# 单股分析
optix analyze COIN

# 指定参数
optix analyze COIN --weeks=2 --capital=10000 --risk=moderate

# 批量分析（自选股所有标的）
optix analyze --watchlist --capital=50000
```

**参数说明：**

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--weeks` | `2` | 预测周期（周），决定期权到期日选择 |
| `--capital` | `50000` | 可用资金（美元），用于仓位计算 |
| `--risk` | `moderate` | 风险偏好：`conservative` / `moderate` / `aggressive` |
| `--watchlist` | `false` | 批量分析自选股全部标的 |
| `--analysis-addr` | `localhost:50052` | Python 分析引擎地址 |

**输出结构：**

```
━━━━ COIN Analysis Report ━━━━

【股票概况】
  价格 / 52周高低 / 成交量 / 换手

【技术面分析】
  趋势方向 / 趋势评分 / 趋势描述
  MA20 / MA50 / MA200
  RSI(14) / MACD / 布林带
  支撑位（来源+强度）
  阻力位（来源+强度）

【期权面分析】
  当前 IV（校正后）/ HV20
  IV Rank / IV Percentile / IV 环境
  最大痛点 / 到期日
  PCR（OI）/ PCR（成交量）
  OI 大量堆积（期权墙）

【市场展望】
  方向 / 置信度
  14天1σ价格区间 / 2σ价格区间
  预测依据

【策略推荐 Top 3】
  策略名称 [得分/100]
    腿位：买/卖 期权类型 行权价 到期日
    权利金：净信用/净成本
    最大盈/亏 ｜ 盈亏比
    胜率 ｜ 损益平衡点 ｜ 保证金
    推荐理由 / 风险提示
```

---

### 4.4 `optix server`

启动 Web UI 后端服务器（含后台调度器）。

```bash
# 默认地址
optix server

# 指定监听地址
optix server --web-addr 127.0.0.1:8080

# 或使用快捷二进制（等同于 optix server）
./bin/optix-server --web-addr 127.0.0.1:8080
```

**参数说明：**

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--web-addr` | `127.0.0.1:8080` | HTTP 监听地址 |
| `--analysis-addr` | `localhost:50052` | Python 分析引擎地址 |

启动后：
- Web UI 可通过浏览器访问 `http://127.0.0.1:8080`
- 后台调度器自动采集 IB 行情、历史 K 线、期权链，并调用 Python 引擎分析
- 所有数据缓存至 SQLite，前端异步轮询获取最新结果

---

### 4.5 `optix dashboard`

查看自选股概况表，快速发现高机会标的，每日快照存入数据库。

```bash
# 基础用法
optix dashboard

# 按 IV Rank 排序，只显示前5名
optix dashboard --sort=iv-rank --top=5

# 指定资金量
optix dashboard --capital=100000
```

**参数说明：**

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--capital` | `100000` | 可用资金 |
| `--sort` | `opportunity` | 排序方式：`opportunity` / `iv-rank` / `trend` / `pcr` |
| `--top` | `0`（全显） | 只显示前 N 行 |
| `--analysis-addr` | `localhost:50052` | Python 分析引擎地址 |

**输出示例：**

```
┌────────┬─────────┬───────┬──────┬─────────┬──────────┬───────┬───────────────────┬───────────────────────┐
│ Symbol │  Price  │ Trend │  RSI │ IV Rank │ Max Pain │  PCR  │  2W Range (1σ)    │  Recommendation       │
├────────┼─────────┼───────┼──────┼─────────┼──────────┼───────┼───────────────────┼───────────────────────┤
│ ★ COIN │  195.53 │  →    │ 56.2 │  64.9%  │  187.50  │  0.85 │ $168.65 – $222.41 │ ★ Iron Condor         │
│ ★ MSTR │  330.21 │  ↑    │ 61.4 │  42.3%  │  325.00  │  0.92 │ $290.10 – $370.32 │ ★ Sell Put / Bull Put │
│   AAPL │  218.10 │  ↑    │ 58.7 │  22.1%  │  215.00  │  0.78 │ $208.32 – $227.88 │ Wait (Low IV)         │
└────────┴─────────┴───────┴──────┴─────────┴──────────┴───────┴───────────────────┴───────────────────────┘
```

- `★` = 机会评分 ≥ 50 分
- `→` = 中性趋势；`↑` = 看多；`↓` = 看空

**排序规则：**

| 排序键 | 逻辑 |
|--------|------|
| `opportunity` | 机会评分从高到低 |
| `iv-rank` | IV Rank 从高到低 |
| `trend` | 看多 → 中性 → 看空（按趋势评分绝对值） |
| `pcr` | PCR 偏离中性（1.0）越远排越前 |

---

## 5. 技术指标计算方法

### 5.1 移动平均线（MA）

| 名称 | 周期 | 计算方式 |
|------|------|----------|
| MA20 | 20日 | 简单移动均线（SMA） |
| MA50 | 50日 | 简单移动均线（SMA） |
| MA200 | 200日 | 简单移动均线（SMA） |

```
SMA(n) = (close[-1] + close[-2] + ... + close[-n]) / n
```

### 5.2 RSI（相对强弱指数）

**周期：14日**

```
Δ = close[t] - close[t-1]

avg_gain = EWM(max(Δ, 0), α = 1/14, 最少14个点)
avg_loss = EWM(max(-Δ, 0), α = 1/14, 最少14个点)

RS  = avg_gain / avg_loss
RSI = 100 - 100 / (1 + RS)
```

| RSI 值 | 含义 |
|--------|------|
| > 70 | 超买区域（短期看空信号） |
| 30 ~ 70 | 正常区间（中性） |
| < 30 | 超卖区域（短期看多信号） |

### 5.3 MACD

```
快线 EMA = EMA(close, 12)
慢线 EMA = EMA(close, 26)

MACD Line     = 快线EMA - 慢线EMA
Signal Line   = EMA(MACD Line, 9)
Histogram     = MACD Line - Signal Line
```

| Histogram | 含义 |
|-----------|------|
| 正值且扩大 | 上涨动能增强 |
| 正值且收缩 | 上涨动能减弱 |
| 负值且扩大 | 下跌动能增强 |
| 负值且收缩 | 下跌动能减弱 |

### 5.4 布林带（Bollinger Bands）

**周期：20日，宽度：2倍标准差**

```
中轨 = SMA(close, 20)
σ    = rolling_std(close, 20)

上轨 = 中轨 + 2σ
下轨 = 中轨 - 2σ
```

---

## 6. 波动率体系

### 6.1 历史波动率（HV20）

**计算公式：**

```
log_returns[t] = ln(close[t] / close[t-1])

HV20 = std(log_returns[-20:]) × √252
     = 20日对数收益率标准差 × 年化因子
```

- 计算时使用最近 20 个交易日的对数收益率
- 乘以 √252 年化（一年约 252 个交易日）
- 最低值下限：5%

### 6.2 IV Rank（隐含波动率排名）

**定义：** 当前 HV20 在过去一年历史分布中的相对位置。

```
IV Rank = (当前HV20 - 年度最低HV20) / (年度最高HV20 - 年度最低HV20) × 100%
```

**示例（COIN，2026-03-15）：**
```
HV20 = 93.6%
年度HV20最低 = 45.0%（假设）
年度HV20最高 = 148.0%（假设）

IV Rank = (93.6 - 45.0) / (148.0 - 45.0) × 100% = 47.2%
```

| IV Rank | 等级 | 卖权策略适合度 |
|---------|------|---------------|
| ≥ 50% | HIGH | ✅ 最优 |
| 30% ~ 50% | MEDIUM | ⚠️ 可考虑 |
| < 30% | LOW | ❌ 不推荐卖权 |

### 6.3 IV Percentile（历史分位数）

**定义：** 历史 HV20 中有多少百分比的值低于当前值。

```
IV Percentile = count(HV_series < 当前HV20) / len(HV_series) × 100%
```

IV Percentile 与 IV Rank 的区别：
- IV Rank：基于极值（最高/最低）计算，对异常值敏感
- IV Percentile：基于历史分布密度，更稳健

### 6.4 IV 校正因子（核心设计决策）

**背景问题：**
IB 的 `reqSecDefOptParams` 接口只返回期权链结构（行权价 + 到期日），**不含实时期权买卖价**（需要单独市场数据订阅）。因此系统用 HV20 代替 IV 进行 BS 定价。

**问题：** 对于波动较大的个股，HV20 通常高于市场实际 IV。

**实证验证（COIN，2026-03-13）：**
```
HV20          = 93.6%
市场实际IV    = 70.25%（来源：alphaquery.com）
IV/HV 比率    = 70.25 / 93.6 = 0.75
```

**修正公式：**
```
定价用IV = HV20 × 0.75
```

**适用范围：**
高波动个股、非财报事件期间，IV/HV 比率通常在 0.70 ~ 0.85 之间。

**注意：**
- `定价用IV` 用于：BS 期权定价、希腊值计算、胜率估算、价格区间预测
- `HV20（原始值）` 用于：IV Rank、IV Percentile（自身比较，无需校正）

---

## 7. 趋势评分算法

### 7.1 评分组成（-1 到 +1）

趋势评分由四个维度加权合成：

```
trend_score = MA信号 × 0.35
            + MACD信号 × 0.25
            + RSI信号 × 0.20
            + 成交量信号 × 0.20

最终：clip(trend_score, -1.0, +1.0)
```

#### 维度①：移动均线信号（权重 35%）

```python
信号列表 = []

# 价格与各均线位置关系
price > MA20  → +1.0，否则 → -1.0
price > MA50  → +1.0，否则 → -1.0
price > MA200 → +1.0，否则 → -1.0

# 短中期均线交叉
MA20 > MA50   → +0.5（金叉），否则 → -0.5（死叉）

MA信号 = mean(信号列表)
```

#### 维度②：MACD 信号（权重 25%）

```python
MACD信号 = clip(MACD柱 / |MACD线|, -1.0, +1.0)
```

- MACD 柱为正（MACD 线在信号线上方）→ 正值 → 看多
- MACD 柱为负（MACD 线在信号线下方）→ 负值 → 看空

#### 维度③：RSI 信号（权重 20%）

```python
if RSI > 70:
    RSI信号 = -0.5    # 超买，轻微看空
elif RSI < 30:
    RSI信号 = +0.5    # 超卖，轻微看多
else:
    RSI信号 = (RSI - 50) / 50 × 0.5   # 线性映射：[-0.5, +0.5]
```

#### 维度④：成交量信号（权重 20%）

```python
近5日均量 = mean(volume[-5:])
近20日均量 = mean(volume[-20:])
量比 = 近5日均量 / 近20日均量
价格上涨 = close[-1] > close[-6]   # 5日前

if 量比 > 1.2:                 # 放量
    成交量信号 = +0.3 if 价格上涨 else -0.3
elif 量比 < 0.8:               # 缩量
    成交量信号 = -0.1 if 价格上涨 else +0.1
else:
    成交量信号 = 0.0
```

### 7.2 方向判断

```
trend_score > +0.30  →  "bullish"（看多）
trend_score < -0.30  →  "bearish"（看空）
其余               →  "neutral"（中性）
```

### 7.3 最大痛点与 PCR 修正

在策略推荐阶段，趋势评分会进一步被最大痛点和 PCR 修正：

```python
# 最大痛点修正（资金引力）
mp_bias = (max_pain - current_price) / current_price  # 正=痛点在上方
adj_score += mp_bias × 0.15

# PCR 极端修正（反向指标）
if PCR > 1.5:    adj_score += 0.10   # 极度悲观 → 反向看多
if PCR < 0.5:    adj_score -= 0.10   # 极度乐观 → 反向看空

# 重新分类
adj_score > +0.30 → "bullish"
adj_score < -0.30 → "bearish"
else             → "neutral"
```

---

## 8. 期权分析指标

### 8.1 最大痛点（Max Pain）

**定义：** 所有未平仓期权在哪个行权价到期将给期权买方造成最大损失（即期权卖方获益最大）。

**计算算法：**

```python
for test_price in all_strikes:
    call_pain = Σ max(test_price - K, 0) × call_OI[K] × 100
    put_pain  = Σ max(K - test_price, 0) × put_OI[K] × 100
    total_pain = call_pain + put_pain

max_pain = argmin(total_pain)
```

**市场意义：**
- 做市商为对冲裸卖期权，会在正股上做 delta 对冲
- 临近到期，股价往往向最大痛点收敛（"Pin Risk"）
- 最大痛点通常用于确定支撑/阻力（强度：70）

### 8.2 认沽/认购比（PCR）

```
PCR（OI）  = Σ Put OI / Σ Call OI
PCR（成交量）= Σ Put Volume / Σ Call Volume
```

| PCR | 含义 | 策略含义 |
|-----|------|----------|
| > 1.5 | 极度看空情绪 | 反向信号→看多 |
| 1.0 ~ 1.5 | 偏悲观 | 正常区间 |
| 0.7 ~ 1.0 | 中性 | 中性 |
| < 0.5 | 极度乐观 | 反向信号→警惕回落 |

> IB 提供的结构化期权链暂无成交量数据，因此 PCR（成交量）≈ PCR（OI）。

### 8.3 期权墙（OI Walls）

**认沽墙（Put Wall）= 支撑位：**
- 大量认沽期权堆积的行权价
- 做市商卖空认沽后需在下方买入正股对冲 → 形成买盘支撑
- 强度 = min(OI / 1000, 90)

**认购墙（Call Wall）= 阻力位：**
- 大量认购期权堆积的行权价
- 做市商卖空认购后需在上方卖出正股对冲 → 形成卖盘阻力
- 强度 = min(OI / 1000, 90)

### 8.4 价格区间预测

**基于波动率的统计区间：**

```
T = forecast_days / 365

1σ移动 = IV_定价 × current_price × √T

1σ区间下沿 = current_price - 1σ移动    （约68%概率在此区间内）
1σ区间上沿 = current_price + 1σ移动

2σ区间下沿 = current_price - 2×1σ移动  （约95%概率在此区间内）
2σ区间上沿 = current_price + 2×1σ移动
```

**COIN 示例（2026-03-15，IV=70.2%，14日）：**
```
T = 14/365 = 0.0384
1σ移动 = 70.2% × 195.53 × √0.0384 = $26.88

1σ区间：$168.65 ~ $222.41
2σ区间：$141.77 ~ $249.29
```

### 8.5 支撑/阻力位综合系统

所有来源合并后，按距离当前价格由近及远排列：

| 来源 | 强度 | 说明 |
|------|------|------|
| 短期枢轴点（5根K线窗口） | 60 | 价格局部高低点 |
| MA20 | 20 | 短期均线 |
| MA50 | 25 | 中期均线 |
| MA200 | 50~100 | 长期均线（信号最强） |
| 斐波那契（0%/23.6%/38.2%/50%/61.8%/78.6%/100%） | 50 | 摆动高低点展开 |
| 布林带上/下轨 | 40 | 动态压力/支撑 |
| Put Wall（期权支撑） | min(OI/1000, 90) | 来自OI数据 |
| Call Wall（期权阻力） | min(OI/1000, 90) | 来自OI数据 |
| 最大痛点 | 70 | OI加权计算 |

---

## 9. 策略推荐决策逻辑

### 9.1 总流程图

```
         输入：股票数据 + 期权链
                   │
         ┌─────────▼──────────┐
         │  检查 IV 环境       │
         │  IV Rank < 30%？   │
         └─────────┬──────────┘
          低IV: 是  │  高/中IV: 否
                   │              │
         ┌─────────▼──────────┐  │
         │  返回"等待高IV"     │  │
         │  不推荐卖权策略     │  │
         └────────────────────┘  │
                                  │
                    ┌─────────────▼────────────────┐
                    │  确定趋势方向                  │
                    │  bullish / bearish / neutral  │
                    └──────────┬───────────────────┘
               ┌───────────────┼───────────────┐
           看多│            中性│           看空│
   ┌───────────▼──┐  ┌─────────▼───┐  ┌────────▼──────┐
   │ 卖认沽 /     │  │ 铁鹰策略    │  │ 熊市认购价差  │
   │ 牛市认沽价差 │  │ 宽跨组合    │  │ Bear Call     │
   └───────────┬──┘  └─────────┬───┘  └────────┬──────┘
               └───────────────┼───────────────┘
                                │
               ┌────────────────▼────────────────┐
               │  过滤 & 评分                      │
               │  ①仓位上限过滤 ②盈亏比过滤       │
               │  ③综合评分（0-100分）             │
               └────────────────┬────────────────┘
                                │
               ┌────────────────▼────────────────┐
               │  输出 Top 3 策略推荐              │
               └─────────────────────────────────┘
```

### 9.2 低 IV 处理

```python
if IV_Rank < 30%:
    返回单条建议：
    "Low IV environment (IV Rank < 30%). Sell-side strategies have poor
     risk/reward. Consider waiting for IV expansion."
```

**原因：** 卖权策略的利润来源是时间价值（Theta）和波动率溢价（Vega）。IV 过低时，权利金不足以补偿潜在风险。

### 9.3 行权价选择逻辑

#### 卖出认沽行权价（看多 / 铁鹰下侧）

```python
candidates = []

# 来源1：最近强支撑位
candidates.append(support_levels[0]["price"])

# 来源2：最大认沽OI行权价（Put Wall）
if oi_put_walls:
    candidates.append(oi_put_walls[0][0])

# 来源3：基于 Delta 目标估算
delta_target = {
    "conservative": 0.15,
    "moderate":     0.25,
    "aggressive":   0.30,
}[risk_tolerance]

iv_based = current_price × (1 - delta_target × IV × √T × 2)
candidates.append(iv_based)

# 取中位数，然后四舍五入到最近行权价步长
put_strike = round(median(candidates) / step) × step
```

#### 卖出认购行权价（看空 / 铁鹰上侧）

```python
# 来源1：最近强阻力位
candidates.append(resistance_levels[0]["price"])

# 来源2：最大认购OI行权价（Call Wall）
if oi_call_walls:
    candidates.append(oi_call_walls[0][0])

# 来源3：基于 Delta 目标估算
iv_based = current_price × (1 + delta_target × IV × √T × 2)
candidates.append(iv_based)

call_strike = round(median(candidates) / step) × step
```

#### 行权价步长（由股价决定）

| 股价范围 | 步长 |
|---------|------|
| < $50 | $1.00 |
| $50 ~ $200 | $2.50 |
| $200 ~ $500 | $5.00 |
| ≥ $500 | $10.00 |

#### 价差宽度（由风险偏好决定）

| 风险偏好 | 价差宽度 |
|---------|----------|
| conservative | 步长 × 2 |
| moderate | 步长 × 3 |
| aggressive | 步长 × 4 |

### 9.4 各策略损益计算

#### 卖认沽（Cash Secured Put）

| 指标 | 公式 |
|------|------|
| 权利金（每股） | BS_put(S, K, T, r=5%, IV) |
| 最大盈利 | 权利金 × 100 |
| 最大亏损 | (行权价 - 权利金) × 100 |
| 保证金 | 行权价 × 100 |
| 损益平衡 | 行权价 - 权利金 |
| 胜率估计 | 1 - \|Δput\| |

#### 牛市认沽价差（Bull Put Spread）

| 指标 | 公式 |
|------|------|
| 净信用 | BS_put(卖出腿) - BS_put(买入腿) |
| 最大盈利 | 净信用 × 100 |
| 最大亏损 | (价差宽 - 净信用) × 100 |
| 保证金 | 价差宽 × 100 |
| 损益平衡 | 卖出行权价 - 净信用 |
| 胜率估计 | 1 - \|Δ卖出认沽\| |

#### 熊市认购价差（Bear Call Spread）

| 指标 | 公式 |
|------|------|
| 净信用 | BS_call(卖出腿) - BS_call(买入腿) |
| 最大盈利 | 净信用 × 100 |
| 最大亏损 | (价差宽 - 净信用) × 100 |
| 保证金 | 价差宽 × 100 |
| 损益平衡 | 卖出行权价 + 净信用 |
| 胜率估计 | 1 - Δ卖出认购 |

#### 铁鹰策略（Iron Condor）

| 指标 | 公式 |
|------|------|
| 净信用 | (卖认沽 - 买认沽) + (卖认购 - 买认购) |
| 最大盈利 | 净信用 × 100 |
| 最大亏损 | (max(认沽宽, 认购宽) - 净信用) × 100 |
| 保证金 | max(认沽宽, 认购宽) × 100 |
| 下方损益平衡 | 卖出认沽行权价 - 净信用 |
| 上方损益平衡 | 卖出认购行权价 + 净信用 |
| 胜率估计 | 1 - \|Δ卖出认沽\| - Δ卖出认购 |

> **胜率计算原理：** 使用 BS Delta 作为股价运动到对应行权价以上/以下的概率代理值（风险中性测度下的 N(d₂) 近似）。

#### 宽跨组合（Short Strangle）

| 指标 | 公式 |
|------|------|
| 净信用 | BS_put(认沽腿) + BS_call(认购腿) |
| 最大盈利 | 净信用 × 100 |
| 最大亏损 | current_price × 100 × 0.3（保证金估算） |
| 下方损益平衡 | 认沽行权价 - 净信用 |
| 上方损益平衡 | 认购行权价 + 净信用 |
| 胜率估计 | 1 - \|Δ认沽\| - Δ认购 |

> 宽跨组合在 `risk_tolerance = "conservative"` 时**不推荐**（无保护腿，理论亏损无上限）。

### 9.5 策略过滤条件

```python
# 过滤1：仓位上限
max_position = capital × position_limit[risk_tolerance]
# conservative=10%, moderate=20%, aggressive=30%

if margin_required > max_position:
    丢弃该策略

# 过滤2：最低盈亏比
min_rr = {"conservative": 0.25, "moderate": 0.15, "aggressive": 0.10}

if (max_profit / max_loss) < min_rr[risk_tolerance]:
    丢弃该策略
```

### 9.6 到期日选择

```python
# 最大痛点/OI分析：使用最近到期日（流动性最强）
oi_expiry = option_chain[0]

# 策略腿：选择 DTE ≥ forecast_days / 2 的最近到期日
target_dte = max(forecast_days, 7)   # 至少7天
strategy_expiry = option_chain[0]    # 默认最近
for expiry in option_chain:
    if expiry.days_to_expiry >= target_dte // 2:
        strategy_expiry = expiry
        break

# 示例：forecast_days=14 → 目标DTE=7
# 03-20（5天）不符合 → 选 03-27（12天）✓
```

---

## 10. Black-Scholes 定价与希腊值

**输入参数：**

| 参数 | 符号 | 说明 |
|------|------|------|
| 标的价格 | S | 当前股价 |
| 行权价 | K | 期权行权价格 |
| 到期时间 | T | 年化剩余天数（days/365） |
| 无风险利率 | r | 固定取 5%（美国国债） |
| 波动率 | σ | 定价用IV（= HV20 × 0.75） |
| 股息率 | q | 默认 0（大多数个股未支付股息） |

### d₁ 和 d₂

```
d₁ = [ln(S/K) + (r - q + σ²/2) × T] / (σ × √T)
d₂ = d₁ - σ × √T
```

### 期权价格

```
认购（Call）= S·e^{-qT}·N(d₁) - K·e^{-rT}·N(d₂)
认沽（Put） = K·e^{-rT}·N(-d₂) - S·e^{-qT}·N(-d₁)
```

N(x) = 标准正态分布累积分布函数

### 希腊值

| 希腊值 | Call | Put |
|--------|------|-----|
| **Δ Delta** | e^{-qT}·N(d₁) | e^{-qT}·[N(d₁) - 1] |
| **Γ Gamma** | e^{-qT}·n(d₁) / (S·σ·√T) | 同 Call |
| **Θ Theta** | 复杂公式（见下） | 复杂公式（见下） |
| **ν Vega** | S·e^{-qT}·n(d₁)·√T / 100 | 同 Call |
| **ρ Rho** | K·T·e^{-rT}·N(d₂) / 100 | -K·T·e^{-rT}·N(-d₂) / 100 |

n(x) = 标准正态分布概率密度函数

**Theta（每日时间衰减，以天为单位）：**
```
共同项 = -S·e^{-qT}·n(d₁)·σ / (2·√T)

Call Θ = [共同项 - r·K·e^{-rT}·N(d₂) + q·S·e^{-qT}·N(d₁)] / 365
Put  Θ = [共同项 + r·K·e^{-rT}·N(-d₂) - q·S·e^{-qT}·N(-d₁)] / 365
```

**Delta 的直觉含义：**
- Call Delta ≈ 期权到期变为实值的概率（风险中性测度下）
- 例：Call Delta = 0.30 → 约30% 概率到期在行权价以上
- 卖出 Delta=0.25 的认购 → 约75% 概率到期为虚值（获利）

---

## 11. 评分体系

### 11.1 深度分析评分（analyze 命令）

满分 100 分，五个维度加权：

```python
总分 = 胜率分 × 30 + 盈亏比分 × 25 + 时间价值分 × 20 + IV优势分 × 15 + 安全边际分 × 10
```

| 维度 | 权重 | 归一化范围 | 含义 |
|------|------|----------|------|
| 胜率 (prob_score) | 30% | 50% ~ 95% | 策略到期盈利概率 |
| 盈亏比 (rr_score) | 25% | 0.05 ~ 0.50 | 最大盈利/最大亏损 |
| 时间价值 (theta_score) | 20% | 0 ~ 股价×3% | 净权利金相对股价比 |
| IV优势 (iv_score) | 15% | 30% ~ 90% | IV Rank |
| 安全边际 (safety_score) | 10% | 0 ~ 10% | 损益平衡点到当前价距离 |

**归一化函数：**
```python
normalize(val, min_val, max_val) = clip((val - min_val) / (max_val - min_val), 0, 1)
```

### 11.2 快速扫描评分（dashboard 命令）

用于 Dashboard 快速排序，评分公式更简单：

```python
if IV_Rank < 30%:
    opportunity_score = IV_Rank × 0.5   # 0 ~ 15分，不活跃

elif trend == "bullish" or trend == "bearish":
    # 有方向性
    opportunity_score = min(IV_Rank × 0.6 + |trend_score| × 40, 100)

else:  # neutral
    # 中性，适合铁鹰
    opportunity_score = min(IV_Rank × 0.8, 100)
```

`★`（星标）= 机会评分 ≥ 50 分

---

## 12. 关键阈值速查表

### 波动率阈值

| 指标 | 阈值 | 含义 |
|------|------|------|
| IV Rank | < 30% | LOW：等待 IV 上升 |
| IV Rank | 30% ~ 50% | MEDIUM：可选择性进场 |
| IV Rank | ≥ 50% | HIGH：最佳卖权时机 |
| IV/HV 校正系数 | 0.75 | 期权定价用 IV 估算 |

### 技术指标阈值

| 指标 | 阈值 | 信号 |
|------|------|------|
| RSI | > 70 | 超买（轻微看空） |
| RSI | < 30 | 超卖（轻微看多） |
| 趋势评分 | > +0.30 | 看多 |
| 趋势评分 | < −0.30 | 看空 |
| 趋势评分 | ±0.30 以内 | 中性 |

### PCR 阈值

| PCR | 信号 |
|-----|------|
| > 1.5 | 极度恐惧，反向看多 |
| 1.0 ~ 1.5 | 偏向谨慎 |
| 0.7 ~ 1.0 | 中性 |
| < 0.5 | 过度乐观，警惕 |

### 仓位管理阈值

| 风险偏好 | 单笔仓位上限 | 最低盈亏比 | Delta 目标 |
|---------|------------|----------|----------|
| conservative | 资金 × 10% | 0.25 | 0.15 |
| moderate | 资金 × 20% | 0.15 | 0.25 |
| aggressive | 资金 × 30% | 0.10 | 0.30 |

### 机会评分阈值

| 评分 | 含义 |
|------|------|
| ≥ 50 | 高机会（`★` 星标显示） |
| 30 ~ 50 | 中等机会 |
| < 30 | 低机会 |

---

## 13. 实战使用流程

### 第一步：建立自选股池

**Web UI 方式（推荐）：**
1. 访问 `http://127.0.0.1:8080/watchlist`
2. 在输入框中输入标的代码（空格或逗号分隔），点击"Add"
3. 添加后系统自动触发后台数据采集

**CLI 方式：**
```bash
# 添加标的（优先选择高 Beta、高波动的个股）
optix watch add AAPL TSLA MSFT NVDA AMZN
optix watch add MSTR COIN HOOD GME PLTR  # 高波动候选

# 查看当前列表
optix watch list
```

**高 IV 来源建议：**
- 加密货币相关股：MSTR、COIN、HOOD
- 特殊情况股：GME、AMC
- 高 Beta 科技股：TSLA、NVDA
- 财报前后股票（谨慎）

---

### 第二步：每日 Dashboard 扫描

**Web UI 方式（推荐）：**
1. 访问 `http://127.0.0.1:8080`，Dashboard 会显示所有自选股概况
2. 后台调度器自动采集数据，页面每 10 秒轮询更新
3. 展开底部"📊 Data Freshness"面板查看各标的数据时效性
4. 点击标的链接进入详情分析页

**CLI 方式：**
```bash
# 按机会分数排序
optix dashboard --sort=opportunity --top=10 --capital=50000

# 寻找 ★ 标的（评分≥50）且 IV Rank ≥ 30%
```

**关注信号：**
- `★` 标记 + IV Rank ≥ 50% → 最佳候选
- RSI 在 40~60（中性区间）→ 适合铁鹰
- PCR 在 0.7~1.3（正常区间）→ 无极端情绪
- Max Pain 与当前价接近 → 股价稳定性较高

---

### 第三步：深度分析候选标的

**Web UI 方式（推荐）：**
1. 在 Dashboard 点击标的链接（如 `COIN`）进入 `/analyze/COIN`
2. 查看完整分析报告，包含技术面、期权面、策略推荐
3. 点击"🔄 Refresh live"获取最新 IB 数据重新分析
4. 页面顶部的新鲜度条显示 Quote / OHLCV / Options / Analysis 的最后更新时间

**CLI 方式：**
```bash
optix analyze COIN --weeks=2 --capital=10000 --risk=moderate
```

**决策检查清单：**

```
□ IV Rank ≥ 30%（最好 ≥ 50%）？
□ 策略得分 ≥ 40/100？
□ 胜率 ≥ 25%（对铁鹰而言）？
□ 最大亏损 ≤ 可用资金的 20%？
□ 损益平衡在1σ价格区间外？（宽边际更好）
□ 近期无财报/分红/重大事件？
□ 行权价与关键支撑阻力对齐？
```

---

### 第四步：在 IB TWS 中验证

在开仓前，**必须**在 IB TWS 中核对：

1. **实际期权买卖价** — 系统的 BS 估算价基于 HV×0.75 IV，实际市价可能有差异
2. **Delta 确认** — 在 TWS 期权链中确认卖出腿的 Delta 是否接近目标值（0.15~0.30）
3. **流动性确认** — 买卖价差（Spread）是否合理（< 期权价格的30%）
4. **保证金确认** — 账户实际所需保证金是否满足

---

### 第五步：下单示例（以铁鹰为例）

在 IB TWS 中：

```
组合单（Combo Order）：
- BUY  1  COIN  175P  2026-03-27   （保护认沽）
- SELL 1  COIN  182.5P 2026-03-27  （卖出认沽）
- SELL 1  COIN  205C  2026-03-27   （卖出认购）
- BUY  1  COIN  212.5C 2026-03-27  （保护认购）

限价单：净信用约 $4.00（比系统估算 $4.30 稍低，考虑买卖价差）
```

---

### 第六步：持仓管理

| 情景 | 行动 |
|------|------|
| 盈利已达最大权利金的 50% | 平仓止盈（避免 Gamma 风险） |
| 股价跌破下方损益平衡 | 买回认沽价差，减少亏损 |
| 股价涨破上方损益平衡 | 买回认购价差，减少亏损 |
| 还剩 2~3 天到期 | 若股价在中间区域，继续持有到期 |
| 距离到期 1 周内 Gamma 风险急剧增大 | 提前 7 天考虑平仓或展期 |

---

## 14. 已知局限性与注意事项

### 14.1 数据局限性

| 局限 | 描述 | 影响 |
|------|------|------|
| **无实时期权价格** | IB 结构化期权链不含买卖价（需额外订阅） | BS 估算价 vs 实际市价有差异 |
| **IV 估算误差** | 用 HV20 × 0.75 代替实际 IV | 典型误差 ±5%~10%（校正后接近实际） |
| **历史数据量** | 约 252 个交易日（1年），HV 计算需要至少 20 根K线 | IV Rank 基于1年历史 |
| **无实时成交量** | IB 期权链暂无逐笔成交量数据 | PCR（成交量）= PCR（OI）作为替代 |
| **财报日期** | 系统不自动抓取财报日期 | 手动检查是否有财报在到期日前 |

### 14.2 IV/波动率注意事项

| 情形 | 注意 |
|------|------|
| **财报前** | 实际 IV 可能远高于 HV（IV/HV > 1），系统会低估权利金 |
| **重大事件前** | 同上，IV 溢价（EV）可能很高 |
| **崩盘/大涨后** | HV20 暂时飙升，但市场 IV 可能已回落，系统可能高估权利金 |
| **低波动期** | HV/IV 比率可能 > 0.75，权利金被低估 |

**建议：** 将系统估算值视为**参考区间**，最终以 IB TWS 中的实际买卖价为准。

### 14.3 策略局限性

1. **胜率估算** 使用 Delta 代理，是风险中性概率，不是真实历史频率
2. **保证金** 仅为理论最大亏损估算，实际 IB 保证金要求可能不同
3. **行权价选择** 基于支撑阻力中位数，可能与最优 Delta 略有偏差
4. **宽跨策略** 理论最大亏损无上限，系统使用股价×30% 作为估算保证金

### 14.4 市场风险

- **跳空风险（Gap Risk）**：股价跳过损益平衡点，保护腿也无法完全对冲
- **流动性风险**：远虚值期权买卖价差宽，实际进出场成本高于估算
- **Gamma 风险**：临近到期，Delta 变化加速，小幅股价变动导致大幅P&L波动
- **相关风险**：多个标的同向走势时（如大盘急跌），多个铁鹰同时亏损

---

*本手册基于 Optix Phase 6 版本（2026-03-21）编写。系统用于辅助分析，不构成投资建议。期权交易存在亏损全部保证金的风险。*
