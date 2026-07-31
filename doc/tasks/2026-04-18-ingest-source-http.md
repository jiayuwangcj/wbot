# ingest：`HTTPSource` + `wbot ingest url`

- **id**: `2026-04-18-ingest-source-http`
- **created**: `2026-04-18`
- **updated**: `2026-04-18`

## Goal

补上 `doc/tasks/2026-04-18-ingest-source-file.md` 记录声称已完成、但仓库实际缺失的 HTTP 数据源：`HTTPSource`（GET URL，JSON 数组格式与 `file` 相同）+ CLI `wbot ingest url` + 基于 `httptest` 的单元测。

## Constraints

- 不改变 bars / ingestion_runs schema；无 Redis。
- 解析逻辑与 `file` 共享（抽取公共函数，不复制粘贴）。
- `verify.sh` 无 PG 仍通过。

## Links

- [[ROADMAP]] v1 数据管道
- 前置/纠偏：`doc/tasks/2026-04-18-ingest-source-file.md`（其 State 声称 HTTP url 源已完成，实为未落地）

## State

- **status**: `done`
- **last step**: 已实现 `HTTPSource`（`internal/ingest/http.go`，GET URL 取 JSON 数组，非 2xx/坏 JSON/空数组/空 URL 均报错，ctx 贯穿请求）；`internal/ingest/file.go` 解析循环抽取为公共 `parseBarRecords(data, label)`，错误前缀保留 file/http 来源区别；新增 `http_test.go`（httptest 覆盖正常数组+UTC 归一化、500、坏 JSON、ctx 取消、空 URL）；`cmd/wbot/main.go` 加 `ingest url` 子命令（-dsn/-url 必填/-source cli-url/-symbol/-timeframe/-every）与 usage 文本；`main_test.go` 补 3 个 TestRun 用例。`go test ./internal/ingest/ ./cmd/wbot/ -count=1`、`go vet ./...`、`go build ./cmd/wbot`、`scripts/verify.sh`（无 PG，集成测 skip）全部通过。待主会话 commit + push。

## Next

- commit + push，CI（db-integration job 需 PG）绿后闭环。
