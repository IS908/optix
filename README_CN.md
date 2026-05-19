# Optix

美股期权策略分析工具——基于实时 IBKR 行情和量化分析，识别下一期权到期日的卖方机会。

> English version: [README.md](README.md)

## 概览

Optix 把 Interactive Brokers 的市场数据和 Python 分析引擎结合起来，帮期权卖方找机会：

- **实时行情和期权链** — 通过 IBKR TWS / Gateway
- **技术分析** — SMA / EMA / RSI / MACD / 布林带 / ATR
- **期权定价** — Black-Scholes、希腊字母、隐含波动率、Max Pain
- **策略推荐** — Covered Call、Cash-Secured Put、信用价差、Iron Condor
- **账户持仓 + 交易日记** — 持仓快照含 P&L；持久化成交记录 + FIFO 开平仓配对 + 复盘统计（绕开 IBKR 7 天历史窗口）
- **Web 看板** — 自动刷新、数据新鲜度追踪

> **对 IBKR 只读**。Optix 不会下单、改单、撤单。所有交易操作请用户在 TWS / Gateway 中自行完成。

## 快速上手

### 从 release 安装（推荐普通用户）

到 <https://github.com/IS908/optix/releases> 选最新版本，下载匹配你 OS / 架构的 tarball。包内含预编译 binary、Python 引擎源码、skill 描述文件、`install.sh`。

```bash
VERSION=v0.1.0
OS=$(uname -s | tr '[:upper:]' '[:lower:]')        # darwin | linux
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')

curl -fL "https://github.com/IS908/optix/releases/download/${VERSION}/optix-skill-${VERSION}-${OS}-${ARCH}.tar.gz" \
  | tar -xz
cd "optix-skill-${VERSION}-${OS}-${ARCH}"

# 可选：校验 SHA256
curl -fsSL "https://github.com/IS908/optix/releases/download/${VERSION}/SHA256SUMS" -o SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing

# 安装（自动检测 Claude / OpenClaw / Hermes，也可以用 --agent <list> 显式指定）
./install.sh --agent claude
```

`install.sh` 会把内容铺到 `~/.agents/skills/optix/`（canonical bundle），并在每个 agent 下建 symlink `~/.<agent>/skills/optix`。bundle 的 `.runtime/` 里包含预编译 binary 以及在你机器上现场创建的 Python venv，详见下方 [Agent Skill → 目录结构](#目录结构)。

装完后 binary 可以直接通过这个路径调用：

```bash
~/.agents/skills/optix/.runtime/bin/optix dashboard
~/.agents/skills/optix/.runtime/bin/optix analyze AAPL
~/.agents/skills/optix/.runtime/bin/optix quote TSLA
```

…或者直接让 agent 用，比如对 Claude 说"查一下 AAPL 股价"、"分析 TSLA"。

之后想卸载（不需要保留原始 tarball）：

```bash
~/.agents/skills/optix/install.sh --uninstall --purge
```

### 从源码构建（开发者）

```bash
# 先决条件：Go 1.22+、Python 3.11+（推荐 3.14）、IBKR TWS 或 Gateway

git clone https://github.com/IS908/optix.git
cd optix

# Python 依赖
python3 -m venv python/.venv
python/.venv/bin/pip install -e python/

# 编译 Go 二进制
make build
```

### 从源码运行

```bash
# 终端 1：启动 Python 分析引擎
make py-server

# 终端 2：启动 Web UI（http://127.0.0.1:8080）
./bin/optix-server

# 或者直接用 CLI
./bin/optix dashboard
./bin/optix analyze AAPL
./bin/optix quote TSLA
```

## 架构

```
┌─────────────────────────────────────────────────────┐
│                     用户界面                          │
│         Web UI (:8080)  │  CLI (./bin/optix)         │
└────────────┬────────────┴──────────┬─────────────────┘
             │                       │
┌────────────▼───────────────────────▼─────────────────┐
│                    Go 后端                            │
│  broker/ibkr  │  webui  │  cli  │  datastore/sqlite  │
└───────┬───────┴─────────┴───────┴────────┬───────────┘
        │                                  │
        │ IBKR API                         │ gRPC (:50052)
        │                                  │
┌───────▼───────┐              ┌───────────▼───────────┐
│  IBKR TWS /   │              │   Python 引擎          │
│  IB Gateway   │              │  technical / options /  │
│  (:4001)      │              │  strategy / sentiment   │
└───────────────┘              └───────────────────────┘
```

### 目录结构

```
optix/
├── cmd/                    # 入口
│   ├── optix-cli/          # CLI 二进制
│   └── optix-server/       # Web 服务器二进制
├── internal/
│   ├── broker/ibkr/        # IBKR 集成
│   ├── analysis/           # 调用 Python 引擎的 gRPC 客户端
│   ├── cli/                # Cobra 命令定义
│   ├── datastore/sqlite/   # SQLite 持久化和缓存
│   ├── webui/              # HTTP 服务器、模板、handler
│   ├── scheduler/          # 后台异步刷新
│   └── server/             # 暴露行情数据的 gRPC 服务器
├── python/src/optix_engine/
│   ├── grpc_server/        # gRPC 服务实现
│   ├── options/            # Black-Scholes、Greeks、IV
│   ├── technical/          # 技术指标（SMA / RSI / MACD ……）
│   └── strategy/           # 策略推荐逻辑
├── proto/optix/            # Protobuf 定义
├── skills/commands/optix/  # Claude Code / agent skill
└── docs/                   # 用户手册和设计文档
```

## 使用方式

### CLI 命令

| 命令 | 说明 |
|------|------|
| `./bin/optix dashboard` | 自选股看板：行情 + 技术面 + 策略推荐 |
| `./bin/optix analyze <SYMBOL>` | 深度分析：技术面 + 期权 + 策略 |
| `./bin/optix analyze <SYMBOL> --with-oi [--expiry YYYY-MM-DD]` | 同上，但拉取每张合约的 Open Interest 以计算 Max Pain（需 OPRA 订阅）；`--expiry` 指定具体到期日（默认最近一档） |
| `./bin/optix max-pain <SYMBOL> [--expiry] [--source ibkr\|yfinance\|auto]` | 独立 Max Pain 查询，针对单个到期日 |
| `./bin/optix quote <SYMBOL>` | 实时股票报价 |
| `./bin/optix positions [--type stk\|opt]` | IBKR 账户持仓 + 实时 P&L（必须连 IBKR；期权 mark 需 OPRA 订阅，否则 mark/P&L 列降级为 `—`） |
| `./bin/optix trades [--symbol] [--side] [--since]` | IBKR 近 7 天成交记录（`--since` 超过 7 天会自动截断并提示） |
| `./bin/optix journal status` | 交易日记状态与大小 — **离线安全**，不连 IBKR |
| `./bin/optix journal sync` | 从 IBKR 拉取最近成交写入本地日记（幂等） |
| `./bin/optix journal list [--symbol] [--since] [--until]` | 列出已持久化的成交记录（默认自动同步，可加 `--no-sync` 跳过） |
| `./bin/optix journal trips [--status open\|closed\|expired]` | FIFO 配对的 round trip + 已实现 P&L |
| `./bin/optix journal review [--since] [--until]` | 复盘摘要：胜率、总 P&L、按标的分组 |
| `./bin/optix watch list` | 列出自选股 |
| `./bin/optix watch add <SYMBOL>` | 加入自选股 |
| `./bin/optix watch remove <SYMBOL>` | 移除自选股 |
| `./bin/optix server` | 启动 Web UI |

### Web UI

启动命令 `./bin/optix-server`（默认地址 `http://127.0.0.1:8080`）。

| 路由 | 说明 |
|------|------|
| `/dashboard` | 自选股看板，自动刷新 |
| `/analyze/{symbol}` | 单股深度分析 |
| `/watchlist` | 自选股管理（增删） |
| `/journal` | 交易日记 — Trades / Round Trips / Review 三个标签页 |
| `/help` | 字段说明 |
| `/api/dashboard` | 看板 JSON 接口 |
| `/api/analyze/{symbol}` | 分析 JSON 接口 |
| `/api/journal` | 成交记录 JSON 接口 |
| `/api/journal/trips` | Round trip JSON 接口 |
| `/api/journal/review` | 复盘摘要 JSON 接口 |
| `POST /api/journal/sync` | 触发同步（broker 不可达时返回 502 + `ibkr_ok:false`） |

任意页面追加 `?refresh=true` 可绕过缓存，直接从 IBKR 拉新数据。

Web UI 启动后还会运行交易日记后台同步定时器（`--journal-sync-interval=6h` 默认；`0` 关闭），常驻 `optix-server` 的用户不需要手动 `journal sync` 也能保持在 IBKR 7 天历史窗口内。

### Agent Skill

把 optix skill 装给 AI agent（Claude Code、OpenClaw、Hermes），让它代你执行 quote / analyze / watchlist 等命令：

```bash
# 自动检测本机配置的 agent
./skills/commands/optix/install.sh

# 或显式指定
./skills/commands/optix/install.sh --agent claude
./skills/commands/optix/install.sh --agent claude,openclaw,hermes
```

装好后 skill 会**自动触发**——例如你对 agent 说"查一下 AAPL 股价"、"分析 TSLA"，它会自己识别并调用。在 Claude Code 里也可以用 `/optix <command>` 显式调用。

<a id="目录结构"></a>
#### 目录结构

```
~/.agents/skills/optix/         ← canonical bundle（一份，所有 agent 共享）
├── SKILL.md                     ← skill 描述
├── bin/optix.sh                 ← 入口 wrapper
├── install.sh                   ← 留作日后 --uninstall --purge 用
└── .runtime                     ← symlink (dev) 或 真目录 (release)
    ├── bin/optix
    ├── python/.venv/
    ├── data/optix.db
    └── skills/commands/optix/optix.sh

~/.<agent>/skills/optix → ../../.agents/skills/optix
```

`install.sh` 会自动识别两种安装模式：
- **release** — 从解压的 release tarball 里跑：`.runtime` 是真目录，含预编译 binary 和现场建好的 Python venv
- **dev** — 从源码 checkout 跑（同时存在 `.git` + `Makefile`）：`.runtime` 是指向源码的 symlink，`make build` 改完代码立即生效

如果你有多个 checkout 或者要在多机共享 dotfile，可以用环境变量 `OPTIX_HOME=/path/to/optix` 覆盖 runtime 位置。

#### 卸载

```bash
./skills/commands/optix/install.sh --uninstall --agent claude   # 只删 agent 那条 symlink
./skills/commands/optix/install.sh --uninstall --purge          # 把 canonical bundle 也一起清
```

## 开发

### 编译

```bash
make build          # 同时编 CLI 和 server 二进制
make build-cli      # 只编 CLI（bin/optix）
make build-server   # 只编 server（bin/optix-server）
```

### 测试

```bash
make test               # Go + Python 单测
make test-integration   # 集成测试（自动启动 Python server）
```

### Protobuf

```bash
make proto    # 重新生成 Go / Python 的 proto 代码
```

### 打 release 包（本地 dry-run）

```bash
make release VERSION=v1.2.0                            # 当前平台
make release VERSION=v1.2.0 GOOS=linux GOARCH=arm64
make release-all VERSION=v1.2.0                        # 一次出 4 个平台
```

产物落到 `dist/optix-skill-${VERSION}-${OS}-${ARCH}.tar.gz`。

### IBKR 配置

| 参数 | 默认值 | 命令行参数 |
|------|--------|------------|
| Host | `127.0.0.1` | `--ib-host` |
| Port | `gateway` (4001) | `--ib-port` |

`--ib-port` 接受字符串别名：`gateway`（4001）、`tws`（7496），或一个数字端口（比如 `7497` 是 TWS paper、`4002` 是 Gateway paper）。

## 贡献

### 分支命名

- `feat/<description>` — 新功能
- `fix/<description>` — bug 修复
- `chore/<description>` — 维护、依赖更新
- `docs/<description>` — 文档变更

### Commit 规范

遵循 [Conventional Commits](https://www.conventionalcommits.org/)：

```
feat(webui): add data freshness panel
fix(broker): handle IBKR connection timeout
chore: update Go dependencies
docs: add contributing guide
```

### PR 流程

1. 从 `master` 切一条 feature 分支
2. 用清晰、聚焦的 commit 推进
3. 确保 `make test` 通过
4. 开 PR，写清改动和测试情况
5. 处理 review 反馈

### 代码风格

- **Go** — 用 `gofmt` 标准格式
- **Python** — 用 `ruff` 格式化（`python/.venv/bin/ruff check python/`）
- **Protobuf** — 遵循 [Buf 风格指南](https://buf.build/docs/best-practices/style-guide/)

## Release

每个打 tag 的版本会发到 <https://github.com/IS908/optix/releases>，含 `darwin-{arm64,amd64}` 和 `linux-{amd64,arm64}` 4 个平台的预编译 tarball。完整版本历史见 [`CHANGELOG.md`](CHANGELOG.md)。

**普通用户**：上面 [快速上手 → 从 release 安装](#从-release-安装推荐普通用户) 已给出安装命令。

**Maintainer 发版流程**：

```bash
git tag v1.2.3
git push origin v1.2.3
# .github/workflows/release.yml 会跑 {darwin,linux}×{amd64,arm64} matrix，
# 自动出 release 并附上 4 个 tarball + SHA256SUMS + CHANGELOG.md
```

## License

[MIT](LICENSE)
