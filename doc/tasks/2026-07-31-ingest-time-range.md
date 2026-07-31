# ingest：Source 时间范围参数（-from/-to）

- **id**: `2026-07-31-ingest-time-range`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

历史数据拉取的核心参数：`Source.Bars` 与 `RunIngestion` 增加 `from, to time.Time`（零值 = 不限）；`wbot ingest file|url` 加 `-from`/`-to`（RFC3339）；mock 源忽略范围（demo）；file/url 源按 `[from, to]` 闭区间过滤。

## Constraints

- 不改 schema；`RunMockIngestion` 签名不变（内部传零值范围）。
- `from > to`（两者均非零时）在 `RunIngestion` 校验报错。
- 零值 from/to 不过滤（保留全量语义，向后兼容现有调用/测试）。
- `verify.sh` 无 PG 仍通过。

## Links

- [[ROADMAP]] v1 数据管道
- 前置：`doc/tasks/2026-07-31-ingest-status-query.md`（已完成）
- Driven-By: 主会话按 AUTO_ADVANCE 计划优先级 ④ 拆出的下一最小步（/loop 循环中）

## State

- **status**: `done`
- **last step**: 实现完成并验证。
  - `internal/ingest/bar.go`：`Source.Bars(ctx, symbol, timeframe string, from, to time.Time) ([]Bar, error)`；公共 helper `filterRange(bars, from, to)`（零值不限制；`!from.IsZero() && b.Ts.Before(from)` 与 `!to.IsZero() && b.Ts.After(to)` 过滤；闭区间含端点；可能返回空数组，由 `RunIngestion` 的 "no bars from source" 兜底）。
  - `internal/ingest/mock.go`：`mockSource.Bars` 忽略 from/to（注释注明 demo 源）；`RunMockIngestion` 签名不变，内部传 `time.Time{}, time.Time{}`。
  - `internal/ingest/file.go` / `http.go`：`Bars` 解析后经 `filterRange` 过滤（零值不限制）。
  - `internal/ingest/run.go`：`RunIngestion(..., from, to time.Time, src)`；校验在 nil db/nil src/symbol/timeframe/runSource 之后、取 bars 之前：`!from.IsZero() && !to.IsZero() && from.After(to)` → `errors.New("ingest: from after to")`。
  - `cmd/wbot/main.go`：`runIngestFile`/`runIngestURL` 各加 `-from`/`-to`（RFC3339；空串 = 不限）；新增 `parseRangeTime` helper（空串 → 零值；解析失败 → stderr 提示 + return 2）；usage 文本 JSON 示例行后加 from/to 说明；透传 `RunIngestion(..., fromT, toT, src)`。两子命令保持镜像结构。
  - 测试：`file_test.go`/`http_test.go` 现有 `Bars` 调用补 `time.Time{}, time.Time{}`，新增 `TestFileSource_Bars_range` / `TestHTTPSource_Bars_range`（表驱动：零值全量、闭区间含端点、只 from、只 to、区间外为空）；`run_test.go` 现有调用补零值，新增 `TestRunIngestion_fromAfterTo`（用 `noConnDriver` stub driver 构造非 nil `*sql.DB` 绕过 nil-db 检查，断言错误含 "from after to"）；`main_test.go` 加 `{"ingest file bad from", ...-from not-a-time, 2}` 与 `{"ingest url bad to", ...-to x, 2}`；`integration_test.go` 的 `RunIngestion` 调用补零值（`RunMockIngestion` 不变）。
  - 验证：`go test ./internal/ingest/ ./cmd/wbot/ -count=1` 全绿（无 PG 集成测 skip）、`go vet ./...` 干净、`gofmt -l` 无输出、`scripts/verify.sh` → `verify: ok`。未改 schema、无新依赖；commit/push/CI 由主会话收尾。

## Next

- 已完成：commit `df85670` push 后 run `30618129779` CI **绿**，闭环。后续候选：数据源 Provider 抽象、外部 cron 文档化。
