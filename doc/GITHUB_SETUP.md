# GitHub Setup for Auto-Merge

`main` 分支保护（**已启用**，2026-07-31）：

- PR 才能合入（直接 push 被拒，`GH006`）
- **0 approval（单用户仓库）**：PR 全绿后由 **reviewer subagent**（`.claude/agents/reviewer.md`）多角色评审，通过即合（用户已授权自行评审，2026-07-31）
- 必需 checks：`test`、`db-integration`、`governance`（strict；context 用 job 名，不带 workflow 前缀）
- `Allow auto-merge` 已开启（`gh api` PATCH 仓库设置）

推荐：

- 仅允许 `Squash and merge`
- 禁止直接 push 到 `main`

说明：CI 工作流 `ci` 在 **`test` 同名 job** 内含 `go test`、`go vet`、Linux 上的 `gofmt` / `-race` / `staticcheck`；另有 **`db-integration`**（Postgres + `internal/db` 迁移与 `internal/ingest` 集成测）。PR 上的 `governance`（`Driven-By`）。另有 **`ci-summary`**：在 run 的 **Summary** 面板写入仅由 shell 生成的 Markdown（**无 LLM**），见 [[CI_REPORT]]。`ci-summary` 可不设为必需。可在 Actions 里 **Run workflow** 手动触发（`workflow_dispatch`）。

关联：[[WORKFLOW_GITHUB_DRIVEN]] [[PLAN_V0]] [[AUTO_ADVANCE]] [[FEATURE_SCOPE]] [[README]]
