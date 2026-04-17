# issues（草稿）

尚未在 GitHub 上发帖时，把 **Issue / Discussion 正文草稿** 放在本目录，便于 PR 里链回单一事实来源；发帖后在 Issue 里贴 **Trigger comment** URL。

**已发帖（GitHub 驱动锚点）**：

- Issue [#8](https://github.com/jiayuwangcj/wbot/issues/8) — 通用 `Driven-By` 源：锚点评论 <https://github.com/jiayuwangcj/wbot/issues/8#issuecomment-4268661869>（PR 描述里写 `Driven-By: <该 URL>`）。

在 Cursor 中已启用 **GitHub MCP** 时，Agent 应优先用 MCP 创建/同步线上 Issue 与 Discussion，再把 URL 回填 `doc/tasks/`（见 [[GITHUB_MCP]]）。无 MCP 时可用 `GITHUB_TOKEN` + REST API 或网页创建 Issue/评论。

**Agent 在 GitHub 上发出的评论**须以 **`[robot]`** 开头，见 [[SOURCE_TO_FEATURE]]。

关联：[[AUTO_ADVANCE]] [[GITHUB_MCP]] [[WORKFLOW_GITHUB_DRIVEN]] [[README]]
