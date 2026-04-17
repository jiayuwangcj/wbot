# GitHub 驱动锚点（Issue #8）

- **id**: `2026-04-17-github-driven-anchor`
- **created**: `2026-04-17`
- **updated**: `2026-04-17`

## Goal

- 落实 [[WORKFLOW_GITHUB_DRIVEN]]：线上存在可引用的 **Trigger comment**，并回填 `doc/tasks/`、`doc/issues/`。
- 验收：`Driven-By` 留言 URL 可访问；文档已链到 Issue / 评论。

## Constraints

- 使用仓库 `GITHUB_TOKEN` + GitHub REST API（本环境无 `gh` CLI）。

## Links

- **Driven-By / trigger**: `https://github.com/jiayuwangcj/wbot/issues/8#issuecomment-4268661869`
- Issue: https://github.com/jiayuwangcj/wbot/issues/8

## State

- **status**: `done`
- **last step**：创建 Feature Issue #8、锚点评论、PATCH 正文中的 Trigger 段；更新 `doc/tasks/2026-04-17-v1-first-feature.md`、`doc/issues/*`、`doc/WORKFLOW_GITHUB_DRIVEN.md`。

## Next

- 之后每个 PR 描述含 `Driven-By: …`；可用 #8 评论或新开 Issue 的评论 URL。
