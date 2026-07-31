# v2 第一刀：回测运行器骨架（internal/backtest + `wbot backtest`）

- **id**: `2026-07-31-backtest-runner-slice1`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

按 `doc/issues/draft-2026-07-31-backtest-skeleton.md` 拆出的第一刀：`internal/backtest`（State/Strategy/Runner/Result + 总收益/最大回撤）+ CLI `wbot backtest`（`-file` JSON 输入，`ingest bars -json` 互逆格式；`-cash`；内置 `hold`/`buy-hold` 策略）。

## Constraints

- 不依赖 DB（第一刀纯文件输入）；不改 schema；不引入依赖。
- 确定性：同一输入同一结果（无随机）。
- `verify.sh` 无 PG 仍通过；smoke 用 `backtest -h`。

## Links

- [[ROADMAP]] v2 回测骨架；草稿：`doc/issues/draft-2026-07-31-backtest-skeleton.md`
- 输入格式：`doc/API.md` 的 bars JSON / `ingest bars -json`
- Driven-By: /loop 循环主会话按计划优先级 ④ 拆出（草稿已就绪）

## State

- **status**: `done`
- **last step**: implemented `internal/backtest` (state.go: `State{Cash, Position, Price}` + `Equity(price)`; strategy.go: `Action` hold/buy/sell, `Strategy.OnBar`, `HoldStrategy`, `BuyHoldStrategy` all-in at first close; backtest.go: `Run` with empty-bars / cash<=0 / nil-strategy / `ingest.ValidateBars` checks, per-bar ctx cancel, buy/sell over-trade rejection (1e-9 float tol), equity-curve max drawdown `(peak-trough)/peak`, `ParseBars` reusing the `ingest bars -json` wire format with RFC3339Nano→RFC3339 ts fallback). CLI: `wbot backtest -file -cash -strategy hold|buy-hold`, one summary line, usage text + main_test cases added; verify.sh and ci.yml binary smoke add `backtest -h`. Verified: `go test ./internal/backtest/ ./cmd/wbot/ -count=1`, `go vet ./...`, `scripts/verify.sh` → `verify: ok`; manual 3-bar run: buy-hold 100/110/121 → `final_equity=12100 total_return=0.21 max_drawdown=0`; V-shape 100/50/100/90 → `max_drawdown=0.5`.

## Next

- 已完成：commit `a18dd03` push 后 run `30620928767` CI **绿**，闭环。后续：v2 后续刀（`-dsn` 直读库输入、时间对齐/多 symbol、手续费占位）、小程序前端（blocked 缺工具链）、券商持仓（blocked 缺凭证）。
