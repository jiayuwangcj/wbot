# GitHub Setup for Auto-Merge

`main` 分支建议开启：

- PR 才能合入
- 至少 1 个 approval
- 必需 checks：`ci / test`、`ci / governance`
- 开启 `Allow auto-merge`

推荐：

- 仅允许 `Squash and merge`
- 禁止直接 push 到 `main`

说明：CI 工作流 `ci` 在 **`test` 同名 job** 内含 `go test`、`go vet`、Linux 上的 `gofmt` / `-race` / `staticcheck`，以及 PR 上的 `governance`（`Driven-By`）。分支保护里的必需 check 名称仍为 `ci / test` 与 `ci / governance`。可在 Actions 里 **Run workflow** 手动触发（`workflow_dispatch`）。

关联：[[WORKFLOW_GITHUB_DRIVEN]] [[PLAN_V0]] [[AUTO_ADVANCE]] [[README]]
