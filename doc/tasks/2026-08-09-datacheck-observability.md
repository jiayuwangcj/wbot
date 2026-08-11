# datacheck 只读 API 与 Data 页摘要

- **id**: `2026-08-09-datacheck-observability`
- **created**: `2026-08-09`
- **updated**: `2026-08-09`

## Goal

把 datacheck 当前快照作为只读 HTTP JSON 暴露，并在 Data 页展示按标的汇总的完整/缺失/过期状态，使每日自动 repair 的结果无需登录服务器看日志即可检查。

## Constraints

- 依赖 P0 core 完成后再启动。
- 新增 `GET /v1/datacheck`；禁止新增 HTTP repair/write 端点。
- API 复用 `internal/datacheck`，不得在 httpapi/webui 复制完整性判定。
- Data 页先做摘要与缺失列表，不加入新图表库、不改变现有 K 线交互。
- 单测 + 真 PG 集成测试 + Chrome headless 验收 + `scripts/verify.sh`。

## Links

- Driven-By / trigger: `doc/tasks/2026-08-07-daily-data-completeness.md`
- PR / branch: `codex/feat-datacheck-observability`

## State

- **status**: `done`
- **last step**: Luna 完成只读 API/Data 页实现；主线程修正 PG fixture、错误隔离、补拉后即时刷新与零时间序列化，真实 PG、Chrome headless、最终 verify 全绿。

## Next

P1 已完成。后续按优先级进入 `2026-08-09-datacheck-market-calendar.md`（P2），不在本切片加入 repair 写端点。

## Delivered

- `GET /v1/datacheck`：返回服务端统一计算的 `datacheck.Report`；GET-only，DB 错误 500，空 watchlist 返回空数组。
- Data 页“数据齐全”：标的/完整/缺失/过期摘要，仅列 missing/stale；类型与状态中文化。
- datacheck 错误独立展示，不阻断原有 cluster coverage；手动补 bars/期权成功后立即刷新完整性摘要。
- 零 `time.Time` 使用 Go 1.24 `omitzero`，缺失项不会输出或显示 `0001-01-01`。

## Verification

- `go test ./internal/datacheck ./internal/httpapi ./internal/webui ./cmd/wbot -count=1`：PASS。
- 临时 PostgreSQL 内 `TestHandlerIntegration`：PASS（显式写入/清理唯一 watchlist symbol）。
- Mac Chrome headless：`symbols=1 / complete=0 / missing=24 / stale=1`，中文行与独立 error hidden 正常，无零时间。
- `scripts/verify.sh`：`verify: ok`（含 race/staticcheck/五平台构建/smoke/accept）。
