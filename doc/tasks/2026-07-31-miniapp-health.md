# 小程序所需后端增强：GET /v1/health（含 DB ping）

- **id**: `2026-07-31-miniapp-health`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

`wbot serve` 增加 `GET /v1/health` 健康检查端点（含 DB ping），作为微信小程序④前端的前置健康探测（切片③，miniapp-v1-target）。

## 验收标准（可测）

1. `internal/httpapi`：`Store` 接口新增 `Ping(ctx context.Context) error`；`dbStore` 实现（`db.PingContext`，建议 2-3s 超时上下文）；`Handler` 注册 `/v1/health`——ping 成功 → `200` + JSON `{"status":"ok"}`；ping 失败 → `503` + JSON 错误体（沿用 `writeError`）；非 GET → `405`。
2. 单测（httptest + fake store）：ping ok → 200 且 body 断言；ping err → 503；POST → 405；既有 bars/runs 测试保持全绿。
3. 集成测：有 `WBOT_PG_DSN` 时真 PG + httptest 断言 200 `{"status":"ok"}`；无则 skip（沿用 `integration_test.go` 模式）。
4. `doc/API.md` 增加 `GET /v1/health` 章节（200/503 响应示例）。
5. `go test ./internal/httpapi/ ./cmd/wbot/ -count=1`、`go vet ./...`、`go test -race` 相关包、`scripts/verify.sh` → `verify: ok`。
6. 闭环：verify 绿 → 提交 → push → CI 绿 → reviewer 评审 → 主会话合入 → 更新任务记录 `done` + target 切片③ 标记完成。

## Constraints

- 不改 schema、不引入新依赖（保持标准库 `net/http`）；健康检查只读；无凭证/密钥参与（`PRIVACY` 合规）。
- 挂起项不解除：切片④⑤ 等老板资源（discussions/21）。

## Links

- 源任务：[[2026-07-31-miniapp-v1-target]] 切片③；依赖 ① 后端数据 API（a26436c）✅、② API 契约（b9fbe67）✅
- 契约文档：doc/API.md；实现参考：internal/httpapi/httpapi.go（Store 接口 + dbStore + writeError）
- Driven-By: discussions/10（微信小程序支持）、用户目标「尽快有可用小程序版本」（2026-07-31）

## State

- **status**: `running`
- **last step**: 2026-07-31 dispatcher 派单；worktree `.claude/worktrees/miniapp-health`（分支 `feat/miniapp-health`）已建。

## Next

- coder 实现 → verify → PR → CI 绿 → reviewer 评审 → 主会话合入 → 本记录 `done` + target 切片③ 标记完成。
