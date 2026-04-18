# ingest：`Source` 抽象 + 文件 bars

- **id**: `2026-04-18-ingest-source-file`
- **created**: `2026-04-18`
- **updated**: `2026-04-18`

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
- **last step**: `RunIngestion` + `Source`；`FileSource`（JSON）；`wbot ingest file`；单元测与 `TestRunFileIngestionIntegration`。

## Next

- HTTP(S) mock 源；或调度触发 ingest。
