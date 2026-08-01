# （草稿）GitHub Issue 正文 — 回测结果读取 API（v4 阶段 A 切片 1）

**建议标题**：`[feature] 回测结果读取 API：GET /v1/backtests 列表 + 详情（含 equity 序列）`

---

## Goal（对齐 [[ROADMAP]] v4「控制面与产品化」+ 产品体验意见 2026-08-02 项 1「策略结果可视化」）

`backtest_results`（migration 003）已落库指标（metrics JSONB），但**只有 CLI `-save` 写、无 HTTP 读**——Web 端无法展示策略结果。本切片补齐读取数据面，并为 S2 结果可视化页提供 equity 序列/交易记录：

1. **migration 004**：`backtest_results` 加 `equity_curve jsonb`（`[{ts, equity}]`）、`trades jsonb`（`[{ts, action, symbol, size, price, cash_after}]`）两列，可空（老行兼容）。
2. **runner 暴露序列**（`internal/backtest`）：现有 `Run` 内部已计算 equity 曲线（max_drawdown 计算源），新增确定性输出（新返回结构或 `RunWithTrace`，**不破坏既有 `Run` 签名与输出**）；trades 为**新增记录**（同输入同输出，确定性单测）。
3. **SaveResult 扩展**（`internal/db`）：写入 equity_curve/trades；无序列时（老调用方）metrics-only，向后兼容。
4. **HTTP API**（`internal/httpapi` 新文件 `backtests.go`，独立 handler，不动既有 Store）：
   - `GET /v1/backtests?symbol=&strategy=&limit=`：列表 `[{id, strategy, symbol, params, metrics, start_ts, end_ts, created_at}]`（摘要，不含曲线）。
   - `GET /v1/backtests/{id}`：详情含 `equity_curve`/`trades`；不存在 404。

### 验收（可测）

- 确定性单测：runner 序列输出与手工可核对样例一致（mkBars 风格）；SaveResult 往返（写→读→字段齐全；老行无曲线不报错）。
- httptest 契约测：列表/过滤/详情/404；错误体统一 `{code, message, action}`（本切片起新错误约定，`action` 如「先 `wbot ingest futu -symbol X` 再重跑」——S5 统一全量接入）。
- 集成测（有 `WBOT_PG_DSN` 时真 PG）：`wbot backtest -save` 后经 API 读回，指标与 CLI 输出一致；无 PG 自动 skip（沿用既有模式）。
- `doc/API.md` 增 `/v1/backtests` 章节；`serve -h` 同步（main_test 断言）；`go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`；CI 绿。

### 非目标

- 前端结果页（S2）、一键回测执行端点（S4，POST 不属本切片）、参数对比（S5）、metrics 结构变更。

### 依赖

- 003 migration 的 `backtest_results` 表、`internal/db` SaveResult/LoadResults（47260c0）。
- 与 S3（watchlist 工作台页）并行：S1 改 `internal/backtest`+`internal/db`+`internal/httpapi`，S3 只改 `internal/webui/web`——文件面零重叠。

## 仓库内链回

- 目标切片：`doc/tasks/2026-07-31-web-v1-target.md`（今日目标已达成，v4 阶段 A 新增）
- 现状：`internal/db/migrations/003`（backtest_results）、`cmd/wbot/main.go` runBacktest -save（L530）、`doc/BACKTEST.md`（单行摘要输出）
- 需求源：产品体验意见 2026-08-02 项 1（策略结果可视化，老板「产品体验给出意见」指令）；[[ROADMAP]] v4

## Plan（可勾选）

- [ ] migration 004 + SaveResult 扩展 + runner 序列/交易输出（确定性单测）
- [ ] `internal/httpapi/backtests.go` GET 列表/详情 + 错误体 code/action + 契约测
- [ ] API.md + serve -h + verify/CI 绿


## 状态（2026-08-02）

✅ **已完成并合入**：S1（#73）、S2（#74）、S3（#72）、S4（#75）、S5（#76）——策略回测工作台里程碑完整交付（watchlist 调参 → 一键回测 → 结果查看/对比全 Web 闭环）。
