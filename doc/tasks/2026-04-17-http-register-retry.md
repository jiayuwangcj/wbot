# HTTP 登记重试与可观测错误类型

- **id**: `2026-04-17-http-register-retry`
- **created**: `2026-04-17`
- **updated**: `2026-04-17`

## Goal

- 对齐 `2026-04-17-http-register-transport` 的 Next：对**可恢复**登记失败做有限次重试；非 OK 响应用 `HTTPError` 表达。
- 验收：`./scripts/verify.sh` 通过。

## Constraints

- 不引入新依赖；不重写 `internal/poll` 语义。

## Links

- Driven-By / trigger: 用户 `continue`（[[AUTO_ADVANCE]]）
- PR / branch: main

## State

- **status**: `done`
- **last step**：`Client` 增加 `RetryMax` / `RetryBackoff`、503/429/网络重试；`agent -master-url` 默认 2 次额外重试；单测 `TestRegisterRetries503` / `TestRegisterNoRetryOn400`。

## Next

- 可选：TLS；或 stderr 轻量日志（引入 logger 后再接）。
