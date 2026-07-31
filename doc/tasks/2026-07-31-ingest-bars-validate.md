# ingest：bars 落库前完整性校验

- **id**: `2026-07-31-ingest-bars-validate`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

落实 [[ROADMAP]] v1「校验」方向：`RunIngestion` 落库前对 Source 返回的 bars 做**合法性校验**（OHLC 关系 + 时间单调），拒绝脏数据入库并返回错误。

## Constraints

- 不改 bars / ingestion_runs schema；无 Redis；不改 mockSource 数据。
- 校验规则取保守默认（只拒绝明确非法的数据，不做间隔一致性等严约束）：high >= max(open, low, close)？——实现时按「high ≥ open/low/close、low ≤ open/close」与 ts 严格单调递增（重复 ts 也拒绝，因为 ON CONFLICT 会吞掉但校验应报出）。
- 错误信息可定位：指出第几条 bar 与原因。
- `verify.sh` 无 PG 仍通过。

## Links

- [[ROADMAP]] v1 数据管道
- Driven-By: 主会话按 AUTO_ADVANCE 计划优先级 ④ 拆出的下一最小步（GitHub ② 档无未闭环可执行项：#8 为 Driven-By 锚、#1-5 为 roadmap 占位）

## State

- **status**: `done`
- **last step**: `internal/ingest/bar.go` 新增 `ValidateBars(bars []Bar) error`：空数组拒绝；每条 bar 校验 ts 非零与 OHLC 关系（high < low/open/close、low > open/close）；相邻 bar ts 严格递增（重复 ts 拒绝）；错误含 `ingest:` 前缀、bar 索引与原因。`run.go` 的 `RunIngestion` 在 `src.Bars` 后、`BeginTx` 前调用，失败透传（保留既有 no bars 检查作双保险，注释说明）。新增 `bar_test.go` 表驱动 10 用例（合法、空数组、high<low、high<open、high<close、low>open、low>close、重复 ts、乱序 ts、零值 ts）。验证：`go test ./internal/ingest/ ./cmd/wbot/ -count=1` 全过，`go vet ./...` 干净，`scripts/verify.sh` → `verify: ok`（无 PG 集成测 skip）。

## Next

- 已完成：commit `79f9157` push 后 run `30615661043` CI **绿**，闭环。后续可接数据源 Provider 抽象、`ingest` 状态查询（可观测性）、时间范围参数、外部 cron 文档化。
