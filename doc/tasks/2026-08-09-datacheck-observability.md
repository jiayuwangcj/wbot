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

- **status**: `running`
- **last step**: P0 已独立提交并验证；P1 分支已创建，准备派发 Luna subagent。

## Next

Luna subagent 从 httpapi handler/fake store 测试开始，随后接 mux、API 文档与 Data 页；主线程负责复核、真实 PG/Chrome 与最终 verify。
