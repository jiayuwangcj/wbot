# HTTP 登记传输（agent→master 最小切片）

- **id**: `2026-04-17-http-register-transport`
- **created**: `2026-04-17`
- **updated**: `2026-04-17`（CLI：master HTTP + agent `-master-url`）

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
- **last step**：`wbot master -listen` 提供 HTTP 登记；`wbot agent -master-url` 经 `httpregister.RemoteFacade` 走 `poll.Run`；`TestAgentMasterURL`；`verify.sh` / CI smoke 含 `master -duration 1ms`。

## Next

- 可选：TLS；或把 HTTP client 更深接入 `Heartbeat` 路径的观测与重试。
