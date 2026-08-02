# （草稿）GitHub Issue 正文 — 回测结果导出：equity_curve/trades CSV/JSON 下载

**建议标题**：`[feature] 回测结果导出：GET /v1/backtests/{id}/export?format=csv|json + CLI backtest -export`

---

## Goal（对齐 [[ROADMAP]] v4「控制面与产品化」+ 产品体验意见 2026-08-02 项 1「策略结果可视化」的数据出口）

工作台结果页（S2）已可看曲线/对比（S5），但**无数据出口**——外部工具（Excel/脚本）分析与报告存档需要导出。`GET /v1/backtests/{id}` 已返回完整 JSON（equity_curve/trades，migration 004），导出本质是序列化 + 下载头，成本低收益直接。

### 范围（建议的最小拆法：2 个子切片）

#### 子切片 A：`GET /v1/backtests/{id}/export?format=csv|json`

- `csv`：equity_curve（`ts,equity`）+ trades（`ts,action,symbol,size,price,cash_after`）——**合表 vs 两个文件待拍板**（产品建议：单响应多段 CSV 用空行分隔，简单可解析；评审确认）。
- `json`：与 `GET /v1/backtests/{id}` 详情一致（等价输出，契约对齐）。
- 响应头：`Content-Disposition: attachment; filename="backtest-{id}-{strategy}-{date}.csv"`；老行无曲线（equity_curve 为 NULL，S1 兼容语义）→ 200 + 仅 metrics 表或 404（**待拍板**，产品建议：200 + 空曲线段 + 提示字段，保持读取兼容）。
- 错误沿用 `{code, message, action}` 约定（S5）；404 不存在 id。
- 默认 format=csv。

#### 子切片 B：CLI `wbot backtest -export -id N -out <dir>`

- 复用 `internal/db` LoadResults（与 API 同源）：写 `backtest-{id}.csv/.json` 到 `-out`；与 `GET export` 输出**同输入同输出**（roundtrip 断言）。
- 无 id → 用法错误 exit 2；id 不存在 → exit 1 + 可读错误。

### 验收（可测）

- 确定性单测：export 内容与 `GET /v1/backtests/{id}` 详情 JSON 字段一致（roundtrip）；CSV 行数 = equity_curve 数组长度、表头正确；无曲线行 → 定义的兼容行为（200 空段/404 二选一，断言落地）。
- httptest：200 + `Content-Type: text/csv`（或 application/json）+ `Content-Disposition` 文件名断言 + 内容抽样断言；404 不存在 id；错误体含 `code/action`。
- CLI 集成测（有 `WBOT_PG_DSN` 真 PG，无则 skip）：`wbot backtest -save` → `-export -id` 文件存在且内容与 API 一致。
- `doc/API.md` 增 export 章节；`doc/BACKTEST.md` 增导出用法；`serve -h`/`backtest -h` 同步（`main_test` 断言）；`go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`；CI 绿。

### 非目标

- 图片/图表报告导出（可视化产物属后续切片；dataviz 不在本切片）。
- 批量导出全部结果（`?limit` 批量走列表页后续）。
- 期权腿单独文件（随 main 文件同表导出即可）。
- 回测执行端点（S4 已交付，无新执行逻辑）。

### 依赖

- **无外部依赖**。
- 前置复用：`GET /v1/backtests/{id}`（S1 #73）、migration 004（equity_curve/trades 列）、`internal/db` SaveResult/LoadResults、错误体约定（S5 #76）。

## 仓库内链回

- 目标切片：v4 阶段 A 工作台（S1-S5 已完成，#72-#76）；ROADMAP v4
- 现状：`internal/httpapi/backtests.go`（详情 handler）、`internal/db/migrations/004`、`doc/API.md`（/v1/backtests 章节）、`doc/BACKTEST.md`
- 需求源：产品组自主任务生成（2026-08-02，PM 队列扫描为空授权）；产品体验意见 2026-08-02 项 1（结果可视化——展示已做，导出为其数据出口）

## Plan（可勾选）

- [x] A：`GET /v1/backtests/{id}/export`（csv/json 序列化 + 下载头 + 404/兼容语义）+ httptest
- [x] B：CLI `backtest -export`（复用 LoadResults）+ 集成测 roundtrip
- [x] API.md + BACKTEST.md + serve/backtest -h + verify/CI 绿

## 状态（2026-08-03）

✅ **已完成并合入**：A `GET /v1/backtests/{id}/export`（csv/json +
下载头）、B CLI `backtest -export`、API.md/BACKTEST.md 同步均已
落地。闭环记录见 PR #87（本体）、`doc/tasks/2026-08-02-backtest-export-ui.md`
（Web 导出入口）。
