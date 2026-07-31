# ingest：`wbot ingest status` 查询最近 ingestion runs

- **id**: `2026-07-31-ingest-status-query`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

数据管道可观测性小步：`wbot ingest status` 子命令列出最近 `ingestion_runs`（id/source/status/started_at/finished_at），默认最近 10 条，`-limit` 可调；`internal/ingest` 提供可测的查询函数，集成测接 PG。

## Constraints

- 不改 schema；只读查询，不写任何表。
- CLI 无 dsn 时退出 2（与 mock/file/url 一致）；`-h` 显示用法。
- `verify.sh` 无 PG 仍通过（CLI 层只测参数路径；查询逻辑集成测 skip 于无 DSN）。

## Links

- [[ROADMAP]] v1 数据管道（可重复落地的可观测性）
- 前置：`doc/tasks/2026-07-31-ingest-bars-validate.md`（已完成）
- Driven-By: 主会话按 AUTO_ADVANCE 计划优先级 ④ 拆出的下一最小步

## State

- **status**: `done`
- **last step**: 实现完成并验证。
  - `internal/ingest/status.go`：新增 `RunStatus`（`FinishedAt *time.Time`，nil 表示仍在运行）与 `RecentRuns(ctx, db *sql.DB, limit int)`；SQL `SELECT id, source, status, started_at, finished_at FROM ingestion_runs ORDER BY id DESC LIMIT $1`；`limit <= 0` 返回 `ingest: status: invalid limit`（`db == nil` 返回 `ingest: status: nil db`）；`finished_at` 用 `sql.NullTime` 扫描转 `*time.Time`；错误统一带 `ingest: status:` 前缀。`internal/ingest/status_test.go` 加 `TestRecentRuns_validation`（limit 0/-1 报错）。
  - `cmd/wbot/main.go`：`runIngest` switch 加 `case "status"`；新增 `runIngestStatus`（`-dsn` 默认回落 `$WBOT_PG_DSN`、`-limit` 默认 10；无 dsn → stderr + 2；open/migrate/查询失败 → 1；成功按 `id source status started_at finished_at` 逐行打 stdout，RFC3339 格式，finished_at 为 nil 打 `-`），结构镜像 `runIngestMock`；`usageIngest` 加 `status` 行。
  - `cmd/wbot/main_test.go`：`TestRun` 表加 `{"ingest status help", ..., 0}`、`{"ingest status no dsn", ..., 2}`。
  - `internal/ingest/integration_test.go`：追加 `TestRecentRunsIntegration`（无 DSN skip；MigrateUp 后 `RunMockIngestion` source="status-test"；`RecentRuns(ctx, database, 10)` 断言 index 0 Source/Status/FinishedAt；`RecentRuns(ctx, database, 0)` 断言报错）。
  - 验证：`go test ./internal/ingest/ ./cmd/wbot/ -count=1` 全绿（无 PG 集成测 skip）、`go vet ./...` 干净、`scripts/verify.sh` 输出 `verify: ok`。

## Next

- 已完成：commit `29e2952` push 后 run `30615892278` CI **绿**（含 db-integration 跑通 `TestRecentRunsIntegration`），闭环。后续候选：数据源 Provider 抽象、ingest 时间范围参数（from/to）、外部 cron 文档化。
