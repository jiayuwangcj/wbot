# ingest：`ingest bars -json` 导出（与 file 源互逆）

- **id**: `2026-07-31-ingest-bars-json-export`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

ROADMAP v1「可选仅开发用的导出目录做比对」收尾：`wbot ingest bars -json` 以 JSON 数组输出已落库 bars（与 `ingest file` 输入同格式），可 `ingest bars -json > out.json` 后 `ingest file -file out.json` 往返 diff。

## Constraints

- 不改既有文本输出（无 `-json` 时行为不变）；不改 schema。
- JSON 字段名与 `internal/ingest/file.go` 的 `fileBarRecord` 一致（ts/open/high/low/close/volume）。

## Links

- [[ROADMAP]] v1 数据管道
- 前置：`doc/tasks/2026-07-31-ingest-bars-query.md`
- Driven-By: /loop 循环主会话按计划优先级 ④ 拆出

## State

- **status**: `done`
- **last step**: `runIngestBars` 加 `-json` flag（`json.NewEncoder` + indent 输出数组，ts RFC3339；usage 文本注明可往返 `ingest file`）；`main.go` 补 `encoding/json` import；`main_test.go` 加 `ingest bars json no dsn → 2`。gofmt/vet/`go test ./cmd/wbot/`/`scripts/verify.sh` 全绿。主会话直改（非 Subagent）。

## Next

- 已完成：commit `5d389df` push 后 run `30618962612` CI **绿**，闭环。v1 数据管道（写/读/导出/校验/调度/可观测）全部完成；后续候选：Provider 抽象（待用户拍板）、v2 回测骨架（新里程碑，需设计）。
