# v1 首个特性切片（domain + agent/master 骨架 + CLI）

- **id**: `2026-04-17-v1-first-feature`
- **created**: `2026-04-17`
- **updated**: `2026-04-17`（GitHub-driven 锚点 Issue #8）

## Goal

- 按 `doc/issues/draft-2026-04-17-v1-first-slice.md` 的 Goal/验收，在仓库内实现 **v1 首包**（domain、agent、master、cmd/wbot），并保持 CI 门禁。
- GitHub 上发帖：**优先**用 Cursor [[GITHUB_MCP]] 按草稿创建 Issue/Discussion 并回填 **Trigger comment** URL；草稿源文件仍是 `doc/issues/draft-2026-04-17-v1-first-slice.md`。

## Constraints

- 不引入真实 HTTP、无券商、无 DB。
- 与现有 `internal/bootstrap` 风格一致；禁止无关大重构。

## Links

- 计划草稿（正文复制源）：[`doc/issues/draft-2026-04-17-v1-first-slice.md`](../issues/draft-2026-04-17-v1-first-slice.md)
- Driven-By / trigger: `https://github.com/jiayuwangcj/wbot/issues/8#issuecomment-4268661869`（[#8](https://github.com/jiayuwangcj/wbot/issues/8) 锚点评论，GitHub API 创建）
- PR / branch: `main` 直推；后续 PR 用 **Driven-By:** 同上或更具体 Issue 评论

## State

- **status**: `done`（代码已落地；线上 Issue 用 MCP 或人工发帖后把 URL 补进 Links）
- **last step**：Subagent 已实现 `cmd/wbot`、`internal/domain`、`internal/agent`、`internal/master`；本地 `go test` / `go vet` / build 通过。

## Next

- 新 PR：描述中含 `Driven-By: https://github.com/jiayuwangcj/wbot/issues/8#issuecomment-4268661869`（或拆新 Feature / Plan Issue 后改用**新评论** URL）；CI governance 将检查 **Driven-By** 字段。

## 停机记录（本回合）

- **当时原因**（历史）：无 `gh` 的 shell 里未直接调用 API。**现已约定**：会话内优先 **[[GITHUB_MCP]]** 发送/获取 Issue 与 Discussion；草稿仍放在 `doc/issues/` 作单一来源。
- **自动化改进**：[[GITHUB_MCP]]、[[AUTO_ADVANCE]]「停机与复盘」、`doc/issues/README` 已对齐。
