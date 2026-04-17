# GITHUB_DISCUSSION_OPS

在本机维护 GitHub Discussions（以及用 REST/GraphQL 做脚本化操作）。

## Cursor 里优先用 GitHub MCP

若 Cursor 已配置 **GitHub MCP**（见 [[GITHUB_MCP]]），**创建 / 列出 / 更新讨论与 Issue** 由 Agent 经 MCP 完成更省事；本节 `curl`/GraphQL 适用于 **无 MCP** 的终端脚本或 CI。Agent 发出的 GitHub **评论**须带 **`[robot]`** 前缀，见 [[SOURCE_TO_FEATURE]]。

## 仅用 API 时（curl）

前置：

```bash
export GITHUB_TOKEN='YOUR_PAT'
```

备注：部分 token 对 `DELETE /repos/.../discussions/{n}` 会返回 **404**，此时用 GraphQL `deleteDiscussion` 更稳。

## 列出讨论

```bash
curl -sS -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer ${GITHUB_TOKEN}" \
  "https://api.github.com/repos/jiayuwangcj/wbot/discussions?per_page=20"
```

## 删除讨论（GraphQL）

把 `DISCUSSION_ID` 换成讨论的 `node_id`（可从 list discussions JSON 里读）：

```bash
DISCUSSION_ID='D_kwDOSFGb3M4AlxMJ'

curl -sS -X POST \
  -H "Authorization: Bearer ${GITHUB_TOKEN}" \
  -H "Content-Type: application/json" \
  https://api.github.com/graphql \
  -d "$(jq -n --arg id "$DISCUSSION_ID" '{query:"mutation($id:ID!){ deleteDiscussion(input:{id:$id}){ clientMutationId }}", variables:{id:$id}}')"
```

## 新建讨论（GraphQL）

需要：

- `repositoryId`：仓库 `node_id`（形如 `R_...`，可在仓库 API JSON 里读）
- `categoryId`：分类 `node_id`（形如 `DIC_...`，可从任意讨论 JSON 里读）

正文建议直接引用文件：`doc/pinned_discussion_body.md`

```bash
REPOSITORY_ID='R_kgDOSFGb3A'
CATEGORY_ID='DIC_kwDOSFGb3M4C7EU5'

jq -n \
  --arg repo "$REPOSITORY_ID" \
  --arg cat "$CATEGORY_ID" \
  --arg title "wbot 协作入口：GitHub 留言驱动" \
  --arg body "$(cat doc/pinned_discussion_body.md)" \
  '{query:"mutation($repositoryId:ID!, $categoryId:ID!, $title:String!, $body:String!){ createDiscussion(input:{repositoryId:$repositoryId, categoryId:$categoryId, title:$title, body:$body}){ discussion{ number url } } }", variables:{repositoryId:$repo, categoryId:$cat, title:$title, body:$body}}' \
| curl -sS -X POST \
  -H "Authorization: Bearer ${GITHUB_TOKEN}" \
  -H "Content-Type: application/json" \
  https://api.github.com/graphql \
  -d @-
```

## Pin

GitHub 目前通常需要在网页里手动 **Pin**（若后续 GraphQL 暴露 pin API，再收敛到自动化）。

讨论的内容如何形成结论并落入仓库、以及与「已阅」的关系：[[SOURCE_TO_FEATURE]]

关联：[[GITHUB_MCP]] [[pinned_discussion]] [[pinned_discussion_body]] [[WORKFLOW_GITHUB_DRIVEN]]
