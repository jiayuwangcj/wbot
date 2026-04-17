# CI 与自动推进流程

- **id**: `2026-04-17-ci-and-auto-advance`
- **created**: `2026-04-17`
- **updated**: `2026-04-17`

## Goal

- 完善 GitHub Actions CI（门禁更完整、行为更可预期）。
- 文档化「无指定目标时」与 Issue/Discussion 需求如何进入计划并自动取下一条（与 `.cursor/rules/supervisor-subagent.mdc` 对齐）。

## Constraints

- 不改变既有「一个 `test` job 名称」的取向，避免已有分支保护里 `ci / test` 需批量改名。
- 不写入密钥；网络仅在 CI 内。

## Links

- Driven-By / trigger: 本会话
- PR / branch: 待开

## State

- **status**: `done`
- **last step**: 已扩展计划优先级（ Issue/Discussion 落账）；已加 `doc/AUTO_ADVANCE.md`；已增强 `ci.yml`（concurrency、`workflow_dispatch`、gofmt、staticcheck、governance 用 `grep`）；已更新 `doc/GITHUB_SETUP.md` 与 `doc/README.md` 入口。

## Next

- 开 PR 时在描述中填 **Driven-By**（可链到本会话或后续 GitHub 评论）；合入后确认 Actions 全绿。
