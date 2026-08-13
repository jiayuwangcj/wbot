# S4 报告数据面 + 基础 CLI

**State**: delivered（2026-08-13；feature，待独立 reviewer 评审与主线合入）
**分支/worktree**: `feat/s4-report-dataplane` / `.claude/worktrees/s4-report-dataplane`
**执行**: Codex（gpt-5.6-luna）

## Goal

单次回测按 `doc/BACKTEST_REPORT.md` schema 1.0 输出版本化 JSON 单一事实源与确定性 Go `html/template` 报告；CLI 直接产出，不接 Discord。

## Delivered

- `wbot backtest -report [-report-dir ./reports]` 输出并覆盖 `{report_id}.json` / `{report_id}.html`；`report_id=bt-{symbol}-{run_seed}-{输入哈希前8位}`。
- 新增 `internal/backtestreport`：`Result` 到 schema 1.0 `single_run` 的结构化映射、UTC `Z` 时间、金额/小数百分比、审计哈希、风险提示和确定性 JSON/HTML。
- S3 扁平未成交数据投影为 `unfilled_model` 对象，包含 `heuristic-1.0`、Bid/bar 假设与 0.55/0.30/0.15 组件；零尝试时 `unfilled_ratio=null`。
- HTML 首屏含能力状态、净收益、最大回撤、未成交率和停止原因；深色 2×2 卡片、折叠明细、Discord Open Graph 元数据；`max-width:430px` 响应式规则覆盖 430px/390px，页面禁止正文横向滚动。
- CLI 单次汇总增加“未成交 N/M (P%)”或“未成交 N/A(无成交尝试)”；多标的明确拒绝 `single_run -report`。
- `doc/BACKTEST.md` 增量说明 `-seed`、报告参数、`Result.Unfilled` 与 `Trade.Filled`；新增零依赖 `scripts/accept-backtest-report.sh` 并纳入 `scripts/verify.sh`。
- `doc/ACCEPTANCE.md` 已按脚本逐个实计更新为 15 个脚本、191 项检查。

## Verification

- `scripts/accept-backtest-report.sh`：连续两遍 11/11 通过（schema、HTML、同输入字节一致、汇总与未成交口径可复算）。
- `scripts/verify.sh`：通过；覆盖前端构建、gofmt、`go test ./...`、`go vet ./...`、`go test -race ./...`、staticcheck、五目标交叉编译、CLI smoke 和零依赖 acceptance。
- PostgreSQL 集成：本切片新增报告验收为文件输入零依赖路径；既有 PG 测试在未设置 `WBOT_PG_DSN` 时按项目约定跳过。

## Next

独立 reviewer 按 feature 类型评审；通过后由主会话决定合入，S7 再消费本报告数据面接 Discord。
