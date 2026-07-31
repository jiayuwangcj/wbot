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

- `internal/backtest/`：`State{Cash, Position float64; Price float64}`（equity = cash + position*price）、`Strategy` 接口（`OnBar(ctx, Bar, *State) (Action, error)`，Action: Buy/Sell/Hold + Size 股数）、内置 `HoldStrategy`/`BuyHoldStrategy`、`Result{Equity, TotalReturn, MaxDrawdown float64; Bars int}`、`Run(ctx, bars, initialCash, strategy) (*Result, error)`（校验：空 bars、initialCash<=0、bars 校验可复用 `ingest.ValidateBars`？—— backtest 包 import ingest 包是否合理（会循环依赖吗？ingest 不依赖 backtest，OK））。最大回撤 = equity 曲线峰值到谷值最大跌幅。CLI `wbot backtest -file <json> -cash 10000 -strategy hold|buy-hold`（-strategy 默认 hold；未知策略 → 2；文件缺失 → 2；解析复用 ingest 的 JSON 格式——backtest 包内解析还是复用 ingest？ingest 的 parseBarRecords 未导出——CLI 层或 backtest 包自解析 JSON（同格式），写清）。输出一行 `final_equity total_return max_drawdown bars`。测试：State 结算、BuyHold 全仓买入/卖出、drawdown 计算（构造 V 形曲线）、空 bars/非法 cash 报错、CLI exit codes（help/无 file/坏 strategy/坏 JSON）。verify.sh 与 ci.yml smoke 加 `backtest -h`。`scripts/verify.sh` 绿 → commit + push → CI 绿闭环。
