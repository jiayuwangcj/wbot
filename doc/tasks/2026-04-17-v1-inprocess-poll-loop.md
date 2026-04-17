# v1 进程内心跳轮询（无 HTTP）

- **id**: `2026-04-17-v1-inprocess-poll-loop`
- **created**: `2026-04-17`
- **updated**: `2026-04-17` (CLI smoke)

## Goal

- 对齐 [[ROADMAP]] v1「agent HTTPS 轮询 master」的**逻辑切片**：定时向 master 登记 agent 身份，全部在进程内完成，无网络。
- 验收：`go test ./...`、`go vet ./...`、本地 staticcheck / race 与 CI 一致。

## Constraints

- 不引入 `net/http` 或 TLS；仅复用现有 `internal/agent`、`internal/master` 门面。

## Links

- Driven-By / trigger: 无（`.` 会话按 [[AUTO_ADVANCE]] 取下一条里程碑最小步）
- PR / branch: 待开

## State

- **status**: `done`
- **last step**：`cmd/wbot agent` 调用 `poll.Run`（`-duration`/`-interval`/`-id`，默认限时；0 duration 走 SIGINT）；`master` 子命令说明 in-process 注册表；测试覆盖。

## Next

- 进入「模拟盘执行骨架」最小类型与测试（或 GitHub Feature Issue + Driven-By URL）。
- 仍需 GitHub 侧 Feature Issue + `Driven-By` URL 时，用 [[GITHUB_MCP]] 或网页发帖并回填 `doc/tasks/2026-04-17-v1-first-feature.md` Links。
