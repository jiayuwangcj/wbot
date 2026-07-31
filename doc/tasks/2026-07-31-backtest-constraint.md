# v2 第四刀：回测约束检查（-max-drawdown）

- **id**: `2026-07-31-backtest-constraint`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

ROADMAP v2「可测的绩效/约束」：`wbot backtest -max-drawdown <p>`（0~1，默认 0=不检查）——结果 MaxDrawdown 超出限制时打印违反信息并 exit 1（脚本化友好）；未设时行为不变。

## Constraints

- 不改 `internal/backtest` 的 `Run`/`Result`（约束判定放 CLI 层或 backtest 包内的纯函数，自定但 `Result` 结构不动）。
- `-max-drawdown` 值域校验：<0 或 >1 → 2。
- 未设（0）时行为与现状完全一致。

## Links

- [[ROADMAP]] v2（「可测的绩效/约束」）
- 前置：`doc/tasks/2026-07-31-backtest-fee-placeholder.md`（已完成）
- Driven-By: /loop 循环主会话按计划优先级 ④ 拆出

## State

- **status**: `done`
- **last step**: 实现完成：`internal/backtest/constraint.go` 新增纯函数 `CheckMaxDrawdown`（limit 非法（<=0 或 >1）→ `backtest: invalid max drawdown limit %v`；res == nil → `backtest: nil result`；MaxDrawdown > limit → `backtest: max drawdown %v exceeds limit %v`；未超限 → nil）。`cmd/wbot/main.go` `runBacktest` 新增 `-max-drawdown` flag（float64，默认 0，描述 "max drawdown limit (0..1); exit 1 when exceeded; 0 = no check"）；from/to 解析后校验值域（<0 或 >1 → stderr + return 2）；Run 成功后打印摘要行之后调 `CheckMaxDrawdown`（仅 `>0` 时），违反 → stderr + return 1；usage 文本加一行。测试：`internal/backtest/constraint_test.go` `TestCheckMaxDrawdown`（直接构造 Result：limit 0.9 → nil、limit 0.5 等于 → nil、limit 0.2 → 错误含 "0.5" 与 "0.2"、nil res → 错误、limit 0 → 错误、limit 1.5 → 错误）；`cmd/wbot/main_test.go` 新增 `backtest bad maxdrawdown high`（1.5 → 2）与 `backtest bad maxdrawdown neg`（-0.1 → 2）。验证：`go test ./internal/backtest/ ./cmd/wbot/ -count=1`、`go vet ./...`、`scripts/verify.sh` 均通过（verify: ok）。

## Next

- 已完成：commit `bd948ca` push 后 run `30621678361` CI **绿**，闭环。后续：多 symbol 时间对齐（需设计拆解）、backtest 用法文档。
