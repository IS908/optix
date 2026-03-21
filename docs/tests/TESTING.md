# 测试说明 - 异步后台刷新系统

## 快速测试

### 1. 启动服务器

```bash
./bin/optix-server
```

预期日志输出：
```
{"level":"info","workers":5,"tick_interval":60000,"message":"Scheduler initialized"}
{"level":"info","workers":5,"tick_interval":60000,"message":"Background scheduler started"}
{"level":"info","worker_id":0,"message":"Worker started"}
{"level":"info","worker_id":1,"message":"Worker started"}
...
Optix web UI  →  http://127.0.0.1:8080
```

### 2. 添加自动刷新符号

1. 浏览器访问：`http://127.0.0.1:8080/watchlist`
2. 点击 "＋ Add Symbol" 按钮
3. 输入符号：`AAPL`
4. **勾选**"启用自动后台刷新"
5. 选择刷新间隔：`5分钟`
6. 点击"添加"

预期结果：
- 页面刷新后显示 "Added: AAPL" 成功消息
- AAPL 行显示绿色徽章：`⏱ 5分钟`

### 3. 验证数据库配置

```bash
sqlite3 data/optix.db "SELECT symbol, auto_refresh_enabled, refresh_interval_minutes FROM watchlist WHERE symbol='AAPL';"
```

预期输出：
```
AAPL|1|5
```

### 4. 观察后台刷新日志

等待 6 分钟（首次刷新需要等一个调度周期），观察终端日志：

预期日志：
```
{"level":"debug","interval":5,"batch_size":1,"total_symbols":1,"message":"Dispatching batch"}
{"level":"info","symbol":"AAPL","worker_id":0,"message":"Background task started"}
{"level":"info","symbol":"AAPL","message":"Fetching quote for AAPL"}
{"level":"info","symbol":"AAPL","message":"Fetching OHLCV for AAPL"}
{"level":"info","symbol":"AAPL","message":"Fetching options for AAPL"}
{"level":"info","symbol":"AAPL","message":"Calling Python analysis for AAPL"}
{"level":"info","symbol":"AAPL","duration":"45.2s","message":"Background task completed"}
```

### 5. 检查后台任务记录

```bash
sqlite3 data/optix.db "SELECT id, symbol, status, created_at FROM background_jobs ORDER BY created_at DESC LIMIT 5;"
```

预期输出：
```
1|AAPL|success|2026-03-21T00:50:00Z
```

### 6. 测试前端自动更新

1. 保持 dashboard 页面打开：`http://127.0.0.1:8080/dashboard`
2. 打开浏览器开发者工具 → Network 标签
3. 观察每 30 秒一次的 `/api/freshness` 请求
4. 等待下一次后台刷新完成（5 分钟后）
5. 观察 AAPL 行是否闪烁绿色动画并更新数据

---

## 常见问题排查

### 问题：Add Symbol 按钮点击无反应

**可能原因**：
- JavaScript 错误（检查浏览器控制台）
- Modal 未正确显示（检查元素审查工具）

**解决方法**：
1. 打开浏览器开发者工具 → Console
2. 刷新页面，查看是否有 JavaScript 错误
3. 点击 "Add Symbol"，查看是否有错误信息

### 问题：Watchlist 显示不全

**可能原因**：
- 数据库缺少 auto_refresh_enabled 列
- SQL 查询未包含新字段

**解决方法**：
```bash
# 检查数据库表结构
sqlite3 data/optix.db ".schema watchlist"

# 应该包含以下列：
# auto_refresh_enabled INTEGER DEFAULT 0
# refresh_interval_minutes INTEGER DEFAULT 15
# last_refreshed_at TEXT
```

### 问题：后台任务不执行

**可能原因**：
- IBKR TWS 未运行或端口错误
- Python 分析服务器未启动
- 符号的 last_refreshed_at 时间戳太新

**解决方法**：
```bash
# 1. 检查 IBKR TWS 是否运行在 7496 端口
# 2. 启动 Python 服务器
make py-server

# 3. 手动触发刷新（强制 last_refreshed_at 为旧时间）
sqlite3 data/optix.db "UPDATE watchlist SET last_refreshed_at='1970-01-01T00:00:00Z' WHERE symbol='AAPL';"
```

### 问题：迁移错误 "duplicate column name"

**已修复**：Migration 002 现已幂等，会先检查列是否存在。

如果仍遇到此问题：
```bash
# 删除数据库重新初始化
rm data/optix.db*
./bin/optix-server
```

---

## 完整端到端测试流程

### 前置条件

- [ ] IBKR TWS 或 IB Gateway 运行在 `127.0.0.1:7496`
- [ ] Python 分析服务器运行在 `localhost:50052`
  ```bash
  make py-server
  ```
- [ ] Optix 服务器已构建
  ```bash
  make build
  ```

### 测试步骤

1. **启动服务器**
   ```bash
   ./bin/optix-server
   ```

2. **添加 3 个符号（不同刷新间隔）**
   - AAPL: 5 分钟自动刷新 ✓
   - TSLA: 15 分钟自动刷新 ✓
   - NVDA: 手动刷新（不勾选自动刷新）

3. **验证 watchlist 显示**
   - AAPL: `⏱ 5分钟` (绿色)
   - TSLA: `⏱ 15分钟` (绿色)
   - NVDA: `手动` (灰色)

4. **等待首次刷新（最多 6 分钟）**
   - 观察日志中 "Background task started: AAPL"
   - 观察日志中 "Background task completed: AAPL"

5. **检查数据库**
   ```bash
   sqlite3 data/optix.db <<EOF
   SELECT symbol, status, retry_count, created_at
   FROM background_jobs
   ORDER BY created_at DESC
   LIMIT 5;
   EOF
   ```

6. **测试前端轮询**
   - 打开 dashboard: `http://127.0.0.1:8080/dashboard`
   - 打开 Network 标签
   - 验证每 30 秒一次 `/api/freshness` 请求
   - 响应应包含：
     ```json
     {
       "watchlist": [
         {"symbol": "AAPL", "cache_at": "2026-03-21T00:50:00Z", ...},
         ...
       ],
       "server_time": "2026-03-21T00:55:00Z"
     }
     ```

7. **测试自动更新动画**
   - 保持 dashboard 打开
   - 等待下一次刷新（5 分钟）
   - 观察 AAPL 行是否：
     - 闪烁绿色背景动画
     - 数据（价格、RSI等）更新
     - **无需手动刷新页面**

8. **测试失败重试**
   - 停止 IBKR TWS
   - 等待下次调度（观察日志错误）
   - 验证 retry_count 增加
   - 重启 TWS，验证最终成功

9. **性能测试（可选）**
   - 添加 20 个符号，全部 15 分钟刷新
   - 监控 30 分钟
   - 验证：
     - CPU < 5%
     - 内存增长 < 50MB
     - 批次分布均匀（~4 批次，每批 5 个符号）

---

## 已知限制

1. **首次刷新延迟**：符号添加后，需等待最多 (刷新间隔 + 1分钟) 才会触发首次刷新
2. **IBKR 依赖**：后台任务要求 IBKR TWS 持续连接，断开会导致任务失败（会自动重试）
3. **Python 依赖**：分析功能要求 Python gRPC 服务器运行
4. **单机限制**：当前 SQLite 后端仅支持单机部署

---

## 回滚方案

如果遇到严重问题需要回滚到之前版本：

```bash
# 1. 切换到 main 分支
git checkout main

# 2. 重新构建
make build

# 3. 删除新数据库（可选，会丢失数据）
rm data/optix.db*

# 4. 重新启动
./bin/optix-server
```

---

**测试完成后**，请填写 [验证清单](docs/superpowers/specs/2026-03-20-async-background-refresh-verification.md)
