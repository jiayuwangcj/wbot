# v2 第二刀：`wbot backtest -dsn` 直读 PostgreSQL

- **id**: `2026-07-31-backtest-dsn-input`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

回测直接消费已落地数据（ROADMAP v2「消费落地数据」）：`wbot backtest -dsn`（回落 `$WBOT_PG_DSN`）经 `ingest.QueryBars` 读库，与 `-file` 互斥二选一；复用 `-symbol/-timeframe/-from/-to/-limit` 参数。

## Constraints

- 不改 schema；`internal/backtest` 包零改动（输入差异只在 CLI 层）。
- `-file` 与 `-dsn` 都空 → 2；都设 → 2；`-dsn` 空且无 env → 2。
- `verify.sh` 无 PG 仍通过。

## Links

- [[ROADMAP]] v2；草稿：`doc/issues/draft-2026-07-31-backtest-skeleton.md`
- 前置：`doc/tasks/2026-07-31-backtest-runner-slice1.md`（已完成）
- Driven-By: /loop 循环主会话按计划优先级 ④ 拆出

## State

- **status**: `done`
- **last step**: `cmd/wbot/main.go` `runBacktest` 已加 `-dsn`（回落 `$WBOT_PG_DSN`）/`-symbol`（DEMO.US）/`-timeframe`（1d）/`-from`/`-to`（复用 `parseRangeTime`，解析失败 → 2）/`-limit`（10000）；互斥校验：file 与 dsn（含 env 回落）皆空 → stderr `set -file or -dsn (or WBOT_PG_DSN)` + 2，皆有 → stderr `-file and -dsn are mutually exclusive` + 2；dsn 分支 `db.Open` → `MigrateUp` → `ingest.QueryBars(ctx, database, symbol, timeframe, fromT, toT, limit)` → `backtest.Run`，失败各带 `backtest: open db:/migrate:/query bars:` 前缀 + 1；file 分支逻辑不动；usage 文本补充 dsn/symbol/timeframe/from/to/limit 说明与「Exactly one of -file and -dsn must be set」。`main_test.go` TestRun 加 3 例：`backtest no input`/`backtest both inputs`/`backtest dsn no value`（均 2）。验证：`go test ./cmd/wbot/ ./internal/backtest/ -count=1`、`go vet ./...`、`scripts/verify.sh` 全绿。未改 internal/backtest、internal/ingest、internal/db、verify.sh、ci.yml；未 commit（主会话提交）。

## Next

- 已完成：commit `60ae95e` push 后 run `30621162775` CI **绿**，闭环。后续：手续费占位、多 symbol 时间对齐、小程序前端（blocked）、券商持仓（blocked）。
