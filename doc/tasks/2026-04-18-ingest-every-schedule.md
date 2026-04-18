# 数据管道：ingest 按间隔重复（调度占位）

- **id**: `2026-04-18-ingest-every-schedule`
- **created**: `2026-04-18`
- **updated**: `2026-04-18`

## Goal

落实 [[ROADMAP]] v1「拉取任务」方向的一小步：`wbot ingest mock|file` 支持 **`-every`** 按间隔重复执行直至 SIGINT；重复写入与 PK 冲突时用 **`ON CONFLICT DO NOTHING`** 保持可重复跑通。

## Constraints

- 不改 bars 表结构；仅调整 INSERT 语句与 CLI。
- `verify.sh` 无 PG 仍通过。

## Links

- [[ROADMAP]]
- 前置：`doc/tasks/2026-04-18-ingest-source-file.md`

## State

- **status**: `done`
- **last step**: `ingest.RunEvery`；`bars` 插入 `ON CONFLICT DO NOTHING`；`wbot ingest mock|file -every`；集成测重复 mock；`scripts/verify.sh` 通过。

## Next

- 数据源 Provider 抽象或外部 cron 文档化；或 ingestion 失败重试策略。
