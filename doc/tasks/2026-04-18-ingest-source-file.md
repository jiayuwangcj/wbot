# ingest：`Source` 抽象 + 文件 bars

- **id**: `2026-04-18-ingest-source-file`
- **created**: `2026-04-18`
- **updated**: `2026-04-18`（HTTP url 源跟进）

## Goal

推进 v1 数据管道：**统一事务写入** `RunIngestion`；`mock` 与 **JSON 文件**两种 `Source`；CLI `wbot ingest file`；单元测 + 既有集成测仍绿。

## Constraints

- 不改变 bars / ingestion_runs schema；无 Redis。
- `verify.sh` 无 PG 仍通过。

## Links

- [[ROADMAP]] v1 数据管道
- 前置：`doc/tasks/2026-04-18-wbot-ingest-cli.md`

## State

- **status**: `done`
- **last step**: `HTTPSource` + `wbot ingest url`（JSON 与 file 同格式）；`http_test` + `TestRunHTTPIngestionIntegration`。

## Next

- 调度：`-every` 已见 `doc/tasks/2026-04-18-ingest-every-schedule.md`；后续可接 Provider 或外部 cron。
