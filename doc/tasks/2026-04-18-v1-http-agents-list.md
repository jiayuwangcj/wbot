# v1：master HTTP 列出已注册 agent（GET /v1/agents）

- **id**: `2026-04-18-v1-http-agents-list`
- **created**: `2026-04-18`
- **updated**: `2026-04-18`

## Goal

在现有 POST `/v1/register` 上增加 **GET `/v1/agents`**，返回 JSON；`httpregister.RemoteFacade` 的 `Agents()` 经 HTTP 拉取列表，便于观测与后续集成测试。

## Constraints

- 不引入新依赖；保持 `scripts/verify.sh` 与 CI 绿灯。

## Links

- Issue #8（v1 锚点）: https://github.com/jiayuwangcj/wbot/issues/8

## State

- **status**: `done`
- **last step**: 增加 GET `/v1/agents`、`Client.ListAgents`、`RemoteFacade.Agents` 走 HTTP；补充测试；`verify.sh` 通过。

## Next

- v1 后续：paper 域扩展（数量/价格）或轮询退避策略等，仍以可测小步迭代。
