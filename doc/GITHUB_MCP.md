# GitHub MCP（Cursor）

在 **Cursor** 中已配置 **GitHub MCP** 时，本仓库的 **Issue / Discussion 的发送与拉取**，Agent **应优先通过 MCP 完成**，而不是假设仅有 `gh` 或裸 `curl`（后者仍可作为脚本/CI 兜底）。

## 本机约定（不在仓库提交密钥）

- 配置入口：用户目录下的 `~/.cursor/mcp.json`（例如启用 `github` 服务端、`Authorization` 与 `${env:GITHUB_TOKEN}`）。
- 令牌：见 `~/.cursor/github-mcp.env.example`（复制为 `github-mcp.env` 并 `chmod 600`），并在启动 Cursor / agent 前加载；校验可用 `~/.cursor/verify-github-mcp-env.sh`。

## Agent 行为

- **创建 / 查询 Issue、Discussion** 时：若当前会话具备 GitHub MCP 工具，**先走 MCP**，把正文与仓库内草稿对齐（如 `doc/issues/*.md`），发帖后把 **URL 与 comment 链接**写回对应 `doc/tasks/*.md` 的 **Links**。
- **评论前缀**：凡由 Agent 经 MCP 在 GitHub 发出的**评论**（Issue 下评论、Discussion 回复、PR 评论等），正文必须以 **`[robot]`** 为前缀（第一行开头），与人工留言区分，见 [[SOURCE_TO_FEATURE]] 中的示例。
- **停机原因**不再是「没有安装 `gh`」——若 MCP 不可用，再按 [[AUTO_ADVANCE]] 的兜底路径（网页粘贴、`gh`、或 [[GITHUB_DISCUSSION_OPS]] 的 API）。

关联：[[WORKFLOW_GITHUB_DRIVEN]] [[AUTO_ADVANCE]] [[GITHUB_DISCUSSION_OPS]] [[README]]
