# 小步提交验证与 CI smoke

- **id**: `2026-04-17-verify-commit-ci-loop`
- **created**: `2026-04-17`
- **updated**: `2026-04-17`

## Goal

- 规则：`doc/tasks` 小步完毕后 **commit 验证**；GitHub Actions **增加验证逻辑**；等 CI 时可停给用户；**CI 已结束且用户无新指令**则继续迭代（见 `supervisor-subagent.mdc`）。
- 验收：`ci.yml` 含 binary + CLI smoke；`scripts/verify.sh` 可本地跑；文档与规则同步。

## Constraints

- 不改变既有 `test` job 名称；governance 行为不变。

## Links

- Driven-By / trigger: 用户规则（本回合）
- PR / branch: 待推送

## State

- **status**: `done`
- **last step**：`ci.yml` 增加 `Verify binary and CLI smoke`；新增 `scripts/verify.sh`；`supervisor-subagent.mdc` 与 `doc/AUTO_ADVANCE.md` 写清提交/等待/超时继续循环。

## Next

- 若有 PR：推送后在 GitHub 上确认 workflow 全绿。
