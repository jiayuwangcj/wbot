# （草稿）GitHub Issue 正文 — v2 回测骨架

**建议标题**：`[feature] v2 回测骨架：消费落地数据的回测运行器 + 最少绩效指标`

---

## Trigger comment

- 仓库级锚点：<https://github.com/jiayuwangcj/wbot/issues/8#issuecomment-4268661869>
- 若单独开 Issue：发帖后在同一 Issue 留锚点评论，把评论 URL 填回此处并作为 PR 的 `Driven-By:`。

## Goal（对齐 [[ROADMAP]] v2）

在 **v1 数据管道（已完成）** 之上建立**消费落地数据的回测运行器**第一刀：加载 bars → 按时间序遍历 → 可测的绩效/约束。**仍不依赖 LLM**。

### 范围（建议的最小拆法）

1. `internal/backtest` 包：
   - `Runner`：输入 bars（`[]ingest.Bar`，ts 已升序）+ 初始资金 + 策略接口（如 `Strategy.OnBar(ctx, Bar, *State)`，`State` 含现金/持仓/equity）；按序逐根 bar 推进，输出 `Result`（equity 曲线终点、总收益、最大回撤）。
   - 设计取舍（Issue 讨论）：策略返回指令（buy/sell/hold）还是回调式；是否引入手续费（第一刀建议：固定金额或 0，明确标注）。
2. **输入来源**：复用 `ingest.Source`（mock/file/url 皆可）或 `ingest.QueryBars`（读库）——第一刀建议 CLI `wbot backtest` 支持 `-file <json>`（`ingest bars -json` 导出的互逆格式）与 `-dsn`（直读 PG）二选一，避免强制依赖库。
3. **CLI**：`wbot backtest -symbol -timeframe -from -to -cash -file|-dsn`，输出一行摘要（final equity / total return / max drawdown / bars count）。
4. **测试**：纯单元测（策略调用次数、现金/持仓结算、总收益与回撤计算、空 bars 报错）；集成测可选（读库路径）。

### 验收

- `go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` 绿；CI（test + db-integration）绿。
- 空 bars / 非法初始资金（<=0）→ 报错退出非零。
- 恒持策略（全程 hold）在已知 mock 数据上的结果可手工核对（确定性）。

### 非目标

- 多 symbol 组合与时间对齐（后续刀）。
- 手续费/滑点模型（除明确标注的占位）。
- LLM 决策（v5）、实盘下单（v3）。

## Plan（可勾选）

- [ ] `internal/backtest`：State/Strategy/Result 类型与单元测
- [ ] `Runner` 主循环 + 绩效计算（总收益/最大回撤）与单元测
- [ ] CLI `wbot backtest`（`-file`/`-dsn` 输入）+ main_test 参数用例
- [ ] 文档：`doc/` 新页或 `doc/DATA_PIPELINE.md` 链接回测用法

## 仓库内链回

- 数据输入：`internal/ingest`（`Source`、`QueryBars`、`ingest bars -json`）
- 任务轨迹：`doc/tasks/2026-07-31-ingest-bars-json-export.md`（v1 收尾）
