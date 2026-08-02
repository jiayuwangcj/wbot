# ingest mock 补 -from/-to 参数(与 file/url 对齐)

- **id**: `2026-08-02-ingest-mock-rangeflags`
- **created**: `2026-08-02`
- **updated**: `2026-08-02`

## Done

- PR #93 已合并:`-from`/`-to` flags + parseRangeTime(非法 → exit 2)+ 透传 RunIngestion(from-after-to 校验生效);mock feed 本身保持固定演示数据(过滤在 source 层)。
- 新增 CLI 用例:`ingest mock bad from`/`bad to` → 2。
- verify 全绿:`go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`;CI(test/db-integration/governance/ci-summary)全 pass。

## Goal

候选「ingest 时间范围参数」的最后缺口:`ingest file`/`ingest url` 已支持 `-from/-to`(RFC3339,闭区间过滤),仅 `ingest mock` 缺失(传 `time.Time{}, time.Time{}`)。补齐使三个 ingest 数据源 CLI 参数面一致。

## Constraints

- 镜像 `runIngestFile` 的既有模式:`-from`/`-to` flag → `parseRangeTime`(非法 → exit 2)→ 透传 `RunIngestion`。
- `mockSource.Bars` 忽略 from/to(固定演示数据,注释不变);`RunIngestion` 的 `from after to` 校验仍生效。
- help 文本注明范围语义(与 file/url 一致措辞)。

## Links

- 候选来源:主会话根任务循环(2026-08-02,ingest 时间范围参数)
- 现状:`cmd/wbot/main.go`(runIngestMock/runIngestFile)、`internal/ingest/run.go`(RunIngestion)

## State

- **status**: `done`
- **last step**: PR #93 merged (df71dd6); CI 全绿;任务落盘。

## Next

- 单测 `cmd/wbot/main_test.go`:`ingest mock bad from` → 2(镜像 `ingest file bad from`)。
- `go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`;CI 绿。
- 独立分支 `feat/ingest-mock-ranges` → PR(`Driven-By` 锚点注释)→ merge。
- 落盘:任务记录 status `done`。
