# Go API 第一刀：`internal/httpapi` + `wbot serve`（GET /v1/bars、/v1/runs）

- **id**: `2026-07-31-httpapi-serve`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

微信小程序前置依赖（ROADMAP v4「Go API」提前实施）：HTTP 只读数据接口，服务已落库的 bars 与 ingestion runs；CLI `wbot serve`；Store 接口注入（单测 fake，集成测真 PG）。

## Constraints

- 只读；不改 schema；不引入新依赖（标准库 net/http）。
- 复用 `internal/ingest` 的 `QueryBars`/`RecentRuns` 作为真实实现；`internal/httpapi` 定义 `Store` 接口（签名一致），不直接依赖 `*sql.DB`（可单测）。
- JSON 字段与 `ingest bars -json` 输出一致（ts RFC3339、open/high/low/close/volume）。
- 参数校验失败 → 400；DB 错误 → 500；未知路径 → 404。
- `verify.sh` 与 ci.yml 的 binary smoke 均加 `serve -duration 1ms`。

## Links

- [[ROADMAP]] v4（Go API 提前）与 v3/v4 分诊：`doc/tasks/2026-07-31-triage-miniapp-priority-raise.md`
- 复用：`internal/ingest/query.go`（QueryBars）、`internal/ingest/status.go`（RecentRuns）、`cmd/wbot/main.go` `runMaster`（listen/shutdown 模式）
- Driven-By: [discussions/9](https://github.com/jiayuwangcj/wbot/discussions/9) 人工留言「提高微信小程序实现的优先级」

## State

- **status**: `done`
- **last step**: 实现 `internal/httpapi`（`Store` 接口 + `Handler` + `NewDBStore`，GET /v1/bars、/v1/runs，JSON 错误体 400/500/404，405 亦 JSON）；`cmd/wbot` 加 `serve` 子命令（`-listen` 默认 127.0.0.1:8080、`-dsn` 回落 `$WBOT_PG_DSN`（无 → exit 2）、`-duration` 0=直到 SIGINT，镜像 runMaster 的 net.Listen + http.Server + Shutdown 模式，启动打 `httpapi: listening on http://<addr>`）；单测（httptest + fake store：bars 成功/默认 limit/缺 symbol/缺 timeframe/坏 from/to/坏 limit/500/404/405，runs 成功含 finished_at null/默认 limit/500）；集成测 `TestHandlerIntegration`（真 PG + httptest，bars len==3 且字段正确、runs 含 httpapi-test；无 WBOT_PG_DSN 时 skip）；main_test 加 serve help → 0、serve no dsn → 2；verify.sh 与 ci.yml binary smoke 加 `serve -h`，ci.yml db-integration 测试行加 `./internal/httpapi/...`。验证：`go test ./internal/httpapi/ ./cmd/wbot/ -count=1` 通过、`go vet ./...` 通过、`go test -race` 通过、staticcheck 通过、`scripts/verify.sh` → `verify: ok`（exit 0）。未 commit（主会话负责提交）。

## Next

- `internal/httpapi/`：`Store` 接口（`QueryBars(ctx, symbol, timeframe, from, to time.Time, limit int) ([]ingest.Bar, error)` 与 `RecentRuns(ctx, limit int) ([]ingest.RunStatus, error)`——签名与 ingest 包一致）；`Handler(store Store) http.Handler`：GET /v1/bars（query 参数 symbol/timeframe 必填、from/to RFC3339 可空、limit 默认 100）、GET /v1/runs（limit 默认 10）；JSON 编码（ts RFC3339）；错误 400/500/404。单测 httptest + fake store（成功/缺参 400/坏时间 400/DB 错 500/404）。`cmd/wbot/main.go` 加 `case "serve"` + `runServe`（`-listen` 默认 127.0.0.1:8080、`-dsn`、`-duration` 0=直到 SIGINT；无 dsn → 2；镜像 runMaster 的 shutdown 模式）。`main_test.go`：serve help → 0、serve no dsn → 2。`scripts/verify.sh` 与 `.github/workflows/ci.yml` binary smoke 加 `"$bin" serve -duration 1ms`。集成测（`internal/httpapi` 或 ingest 风格）：真 PG + httptest 调 GET /v1/bars 断言 200 与 JSON 内容。`scripts/verify.sh` 绿 → commit + push → CI 绿闭环。
