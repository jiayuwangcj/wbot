# wbot CLI：ingest mock

- **id**: `2026-04-18-wbot-ingest-cli`
- **created**: `2026-04-18`
- **updated**: `2026-04-18`

## Goal

提供 `wbot ingest mock`：在设置 `WBOT_PG_DSN` 或 `-dsn` 时执行 `ingest.RunMockIngestion`（含迁移）；无 DSN 时退出码 2，便于无 PG 的本地与 `verify.sh`。

## Constraints

不改变现有 ingest 语义；仅 CLI 胶水。

## Links

- [[ROADMAP]] v1 数据管道
- 前置：`doc/tasks/2026-04-18-data-pipeline-bars-mock.md`

## State

- **status**: `done`
- **last step**: `cmd/wbot` 增加 `ingest`/`ingest mock`、主帮助与单元测。

## Next

- `Source` 与 `wbot ingest file` 见 `doc/tasks/2026-04-18-ingest-source-file.md`；后续：HTTP mock 或调度。
