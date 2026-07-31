# ingest：`-every` 调度失败不终止（失败容忍）

- **id**: `2026-07-31-ingest-every-resilient`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

定时拉取（`wbot ingest mock|file|url -every`）一轮失败（网络抖动、源 5xx）时**不终止整个调度**：打错误日志后等下一个间隔继续；`-every=0` 单次模式保持失败即退出非零。`RunEvery` 语义不变（失败即退），新增失败容忍的调度入口，CLI 三个子命令统一使用。

## Constraints

- 不改 bars / ingestion_runs schema；无 Redis；不改 `RunEvery` 既有语义（其测试必须保持通过）。
- 失败轮次不得伪造成功：错误必须可见（stderr 打印），每轮失败仍推进等待间隔，不无限快速重试。
- `verify.sh` 无 PG 仍通过。

## Links

- [[ROADMAP]] v1 数据管道
- 前置：`doc/tasks/2026-04-18-ingest-every-schedule.md`（Next 指向本方向）
- Driven-By: 主会话按 AUTO_ADVANCE 计划优先级 ④ 拆出的下一最小步

## State

- **status**: `done`
- **last step**: `internal/ingest/loop.go` 新增 `RunEveryResilient(ctx, interval, fn, onErr)`：interval<=0 时单次执行语义与 `RunEvery` 一致（失败直接返回错误、不调 onErr）；interval>0 时循环执行，fn 失败 → `onErr(err)` → 等 interval 继续，ctx 取消返回 `ctx.Err()`。`loop_test.go` 新增 4 个测试（1ms 间隔）：失败 3 次后恢复且 onErr 恰好收到 3 次错误、持续失败直到 ctx 取消且 onErr 每次收到错误、单次模式失败直接返回错误且 onErr 不调用、单次模式成功返回 nil。`cmd/wbot/main.go` 的 `runIngestMock` / `runIngestFile` / `runIngestURL` 三个子命令改用 `RunEveryResilient`，onErr 打印 `ingest <sub>: <err>` 到 stderr 后继续；单次模式外层错误处理（失败退出非零）与成功轮次 `ingest <sub>: ok` 打印保持不变。`RunEvery` 及其测试未改动。验证：`go test ./internal/ingest/ ./cmd/wbot/ -count=1`、`go vet ./...`、`scripts/verify.sh` 全绿（无 PG 集成测 skip）。

## Next

- 已完成：commit `5b9b9cb` push 后 run `30615435030` CI **绿**，闭环。后续可接数据源 Provider 抽象、bars 完整性校验、或外部 cron 文档化。
