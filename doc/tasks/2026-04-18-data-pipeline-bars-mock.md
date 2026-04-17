# 数据管道：bars 表 + mock 导入

- **id**: `2026-04-18-data-pipeline-bars-mock`
- **created**: `2026-04-18`
- **updated**: `2026-04-18`

## Goal

在 [[ROADMAP]] v1 上推进：**OHLCV bars** 表结构；`ingestion_runs` 与 bars 在同一事务内的 **mock 写入**；CI `db-integration` 覆盖 `internal/ingest`。

## Constraints

- 无 Redis；仍仅 PostgreSQL。
- 本地无 `WBOT_PG_DSN` 时单元测可跑、集成测跳过。

## Links

- [[ROADMAP]]
- 前置：`doc/tasks/2026-04-18-data-pipeline-pg-skeleton.md`

## State

- **status**: `done`
- **last step**: 新增 `002_bars.sql`、`internal/ingest`（`RunMockIngestion`）、集成测；扩展 `db-integration` 与 [[GITHUB_SETUP]] 说明。

## Next

- 可选：`wbot` 子命令或定时任务触发 ingest；或引入数据源接口与第二个实现（文件/HTTP mock）。
