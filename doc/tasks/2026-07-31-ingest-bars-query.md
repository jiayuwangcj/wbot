# ingest：`wbot ingest bars` 读已落库 bars

- **id**: `2026-07-31-ingest-bars-query`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

数据管道读写闭合：`wbot ingest bars` 按 symbol/timeframe/`-from/-to`/`-limit` 读取已落库 bars 打印（ROADMAP v1「可选仅开发用的导出目录做比对」的第一步：先能读）。

## Constraints

- 不改 schema；只读查询；`ingest.status`/`ingest file|url` 既有行为不变。
- 校验与 `RecentRuns`/`RunIngestion` 一致：空 symbol/timeframe 报错；`from>to`（均非零）报错；`limit<=0` 报错。
- `verify.sh` 无 PG 仍通过（单测只测校验分支；查询走集成测）。

## Links

- [[ROADMAP]] v1 数据管道
- 前置：`doc/tasks/2026-07-31-ingest-status-query.md`（镜像其模式）
- Driven-By: /loop 循环主会话按计划优先级 ④ 拆出

## State

- **status**: `done`
- **last step**: 实现 `QueryBars`（`internal/ingest/query.go`，校验 db/symbol/timeframe 空、from>to（均非零）、limit<=0，错误均带 `ingest: query bars:` 前缀；条件切片+参数切片动态拼 WHERE，零值 from/to 不加条件，`ORDER BY ts ASC LIMIT $N`）；`cmd/wbot/main.go` 加 `ingest bars` 子命令（`-dsn`/`-symbol` 默认 DEMO.US/`-timeframe` 默认 1d/`-from`/`-to` 复用 `parseRangeTime`/`-limit` 默认 100；无 dsn → 2；open/migrate/查询失败 → 1；stdout 每行 `ts RFC3339 open high low close volume`）并更新 `usageIngest`。测试：`TestQueryBars_validation`（nil db、空 symbol/timeframe、from after to、limit 0/-1，复用 `stubDB` noConn driver）、`TestRun` 加 `ingest bars help → 0`/`bars no dsn → 2`/`bars bad from → 2`、`TestQueryBarsIntegration`（mock 3 条全量 + [中间 ts, 末 ts] 范围 2 条端点含 + limit=1）。验证：`go test ./internal/ingest/ ./cmd/wbot/ -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok` 全绿；CLI smoke：help → 0、无 dsn → 2、bad from → 2。

## Next

- `internal/ingest/` 新增查询函数（如 `QueryBars(ctx, db, symbol, timeframe string, from, to time.Time, limit int) ([]Bar, error)`）：SQL `SELECT ts, open, high, low, close, volume FROM bars WHERE symbol=$1 AND timeframe=$2 [AND ts>=$3] [AND ts<=$4] ORDER BY ts ASC LIMIT $5`（动态条件切片拼参数）；校验：symbol/timeframe 空、from>to（均非零）、limit<=0。`cmd/wbot/main.go` 加 `case "bars"` + `runIngestBars`（`-dsn`/`-symbol` 默认 DEMO.US/`-timeframe` 1d/`-from`/`-to`/`-limit` 默认 100；无 dsn → 2；stdout 每行 `ts open high low close volume` RFC3339）；usage 加 bars 行。测试：校验单测（limit/from>to/空 symbol，参考 `status_test.go` 模式）、`main_test.go` 加 `ingest bars help → 0`/`bars no dsn → 2`/`bars bad from → 2`、集成测 `TestQueryBarsIntegration`（mock 后查 3 条、范围过滤、limit）。`scripts/verify.sh` 绿 → commit + push → CI 绿闭环。
