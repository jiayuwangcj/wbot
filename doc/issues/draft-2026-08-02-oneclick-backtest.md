# （草稿）GitHub Issue 正文 — 一键回测：POST /v1/backtests 执行端点 + Web 触发（v4 阶段 A 切片 4）

**建议标题**：`[feature] 一键回测：服务端执行端点 POST /v1/backtests + 结果页/watchlist 页触发按钮`

---

## Goal（对齐 ⑫ 待拍板项 8「backtest 执行端点：CLI 先行 vs POST /v1/watchlist/{symbol}/backtest API」——CLI 已先行落地，本切片补 API 侧；产品体验意见 2026-08-02 项 3「配置→回测→看结果」闭环）

当前回测执行只在 CLI（`wbot backtest`，`-save` 才落库）；Web 端「配置完 watchlist 后跑回测、看结果」必须切命令行。本切片补**服务端执行**，让工作台闭环：

1. **`POST /v1/backtests`**（`internal/httpapi` 新端点，独立 handler）：
   - body：`{symbol, strategy, params}` 或 `{symbol, from_watchlist: true}`（从 watchlist 行读策略与参数，未配置 404/422）。
   - 复用 `internal/backtest` 运行器 + `internal/db.SaveResult`（含 S1 的 equity/trades 序列）落库；参数校验失败 422（错误体 `{code, message, action}` 约定沿用 S1）。
   - **单进程语义**：互斥（同一时刻只跑一个回测，busy → 409 + `action` 提示稍后重试）；超时（如 5 分钟）→ 504 语义；无数据/无 DSN → 422/503 + 可操作 `action`（如「先 `wbot ingest futu -symbol X -timeframe K_DAY`」）。
2. **Web 触发**：results 页与 watchlist 页加「运行回测」按钮（watchlist 行内：`from_watchlist` 语义；results 页表单：手填 symbol/strategy/params），执行后跳转/刷新结果列表（S2 页）。
3. 同步执行（请求内完成）vs 异步任务：**v1 建议同步 + 互斥**（单进程、回测秒级）；异步任务表属后续（标注非目标）。

### 验收（可测）

- httptest：契约测（mock runner/store）——成功 201/200 + 落库断言、非法参数 422、未配置 watchlist 404、busy 409、错误体含 code/action；无 PG 时 503。
- 集成测（真 PG）：`POST /v1/backtests`（from_watchlist 与手填两态）→ `GET /v1/backtests` 列表出现 → 详情 metrics/equity_curve 非空，与 CLI `wbot backtest -save` 输出一致（同输入同输出）；无 PG 自动 skip。
- 页面断言：watchlist/results 页含对 `POST /v1/backtests` 的 fetch 引用与按钮逻辑；零外部资源回归。
- `doc/API.md` 增 POST 章节；serve -h 同步（main_test）；`go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`；CI 绿。

### 非目标

- 异步任务队列/进度条（单进程同步先行）；并发回测（busy 409 即最小语义）；实盘/模拟盘下单执行（v3）；参数对比（S5）。

### 依赖

- S1（回测结果读取 API：写路径复用其 migration 004 + SaveResult 扩展；GET 列表用于验证闭环）、S3（watchlist 页按钮）、既有 `internal/backtest` 运行器与 `-dsn` 数据读取。
- 并行注意：本切片改 `internal/httpapi`（新文件）+ `internal/webui/web`（按钮）——与 S1/S3 的文件面在 httpapi 上重叠于新增文件，建议 S1 合入后再开 S4，或协调派单顺序。

## 仓库内链回

- 需求源：draft-2026-08-01-strategy-options.md 待拍板项 8（CLI 先行已落地，API 侧本切片补）；产品体验意见 2026-08-02 项 3/6
- 现状复用：`internal/backtest`（Run/RunOptions/OptionsData）、`internal/db` SaveResult、`internal/httpapi/`（watchlist.go 模式）、`doc/BACKTEST.md`（CLI 语义基准）

## Plan（可勾选）

- [ ] POST /v1/backtests 端点（互斥/超时/错误映射）+ httptest 契约测
- [ ] Web 按钮（results/watchlist 页）+ 页面断言
- [ ] API.md + serve -h + 真 PG 集成测 + verify/CI 绿
