# 数据管道：PostgreSQL 连接 + 嵌入式迁移骨架

- **id**: `2026-04-18-data-pipeline-pg-skeleton`
- **created**: `2026-04-18`
- **updated**: `2026-04-18`

## Goal

落地 [[ROADMAP]] v1 第一步：**PostgreSQL** 可连、可迁移；`ingestion_runs` 元表作为占位；CI 在 Linux + Postgres 服务下跑集成测，本地无 DSN 则跳过。

## Constraints

- Redis 不在本切片；仅 `pgx` 驱动 + `database/sql`。
- `scripts/verify.sh` 无本机 PG 仍须通过（跳过后仍 green）。

## Links

- [[ROADMAP]]

## State

- **status**: `done`
- **last step**: 新增 `internal/db`（pgx、`Open`、`MigrateUp`、首条 `ingestion_runs` 迁移）、集成测（`WBOT_PG_DSN`）、CI `db-integration` job；更新 [[GITHUB_SETUP]]。

## Next

- 定义行情/历史 bar 表结构与导入路径；或对接单一数据源 mock。
