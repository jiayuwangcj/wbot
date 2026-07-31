# v2 第三刀：手续费占位（每笔固定费用）

- **id**: `2026-07-31-backtest-fee-placeholder`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

按 v2 草稿「手续费/滑点模型（除明确标注的占位）」：`backtest.Run` 增加每笔固定费用参数（默认 0，向后兼容），buy/sell 成交时从 cash 扣减；CLI `-fee`（float，默认 0）。

## Constraints

- 默认 0 时行为与现状完全一致（既有测试不得改动语义）。
- 费用为负 → 报错；买超/卖超校验考虑费用后的现金（买超判定用 cash >= size*close + fee）。
- `internal/backtest` 签名扩展（`Run(ctx, bars, initialCash, strategy, feePerTrade float64)`）；`internal/ingest`/schema 不动。

## Links

- [[ROADMAP]] v2；草稿：`doc/issues/draft-2026-07-31-backtest-skeleton.md`（非目标注明手续费占位）
- 前置：`doc/tasks/2026-07-31-backtest-dsn-input.md`（已完成）
- Driven-By: /loop 循环主会话按计划优先级 ④ 拆出

## State

- **status**: `done`
- **last step**: `Run` 增加 `feePerTrade float64`（`initialCash` 之后；`fee<0` → `backtest: negative fee`）；buy/sell 成交从 cash 扣/减 fee（Hold 不扣）。买入校验保持既有 `size*close > cash+buyTol`（fee 在结算时扣，buy-hold all-in + fee=1 可通过，final equity = 12099，与任务预期一致；若把 fee 计入校验则 buy-hold 与 `-fee>0` 无法共存——Strategy 接口无 fee 参数）。CLI `runBacktest` 加 `-fee`（默认 0）与 usage 一行。测试：`TestRunFee`（buy-hold fee=1 → 12099；hold fee=1 不扣费）、`TestRunValidation` 加 negative fee 用例；既有用例补 fee=0。`go test ./internal/backtest/ ./cmd/wbot/ -count=1`、`go vet ./...`、`scripts/verify.sh`（verify: ok）全绿。

## Next

- 已完成：commit `4b2cbf1` push 后 run `30621447321` CI **绿**，闭环。后续：多 symbol 时间对齐、更多指标、约束检查（v2 草稿「可测的绩效/约束」）；小程序前端与券商持仓仍 blocked。
