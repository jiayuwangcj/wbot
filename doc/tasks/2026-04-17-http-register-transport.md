# HTTP 登记传输（agent→master 最小切片）

- **id**: `2026-04-17-http-register-transport`
- **created**: `2026-04-17`
- **updated**: `2026-04-17`（实现完成）

## Goal

- ROADMAP v1「agent HTTPS 轮询 master」的**网络前切片**：`net/http` 客户端向 `httptest` 服务端登记 agent id，服务端调用 `master.Facade.Register`。
- 验收：`./scripts/verify.sh` 通过；本切片不强制 TLS（后续再接 HTTPS）。

## Constraints

- 复用现有 `master.Facade`；不引入新依赖；不改 `internal/poll` 语义（可后续把 HTTP client 接进 Heartbeat）。

## Links

- Driven-By / trigger: 会话 `.`（[[AUTO_ADVANCE]]）
- PR / branch: main

## State

- **status**: `done`
- **last step**：新增 `internal/httpregister`：`Handler(master.Facade)` 提供 `POST /v1/register`（JSON `{"id"}`）；`Client.Register`；`httptest` 集成测试；`verify.sh` / race / staticcheck 通过。

## Next

- 可选：`wbot master` 起 HTTP + `wbot agent -master-url`；再接 TLS。
