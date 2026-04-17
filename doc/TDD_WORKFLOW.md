# TDD Workflow

流程：Red -> Green -> Refactor。

最低标准：

- Red：先写失败测试
- Green：最小实现通过测试
- Refactor：重构后测试仍全绿

提交前：

- `go test ./... -count=1`
- `go vet ./...`
- `go test -race ./... -count=1`（建议）

关联：[[WORKFLOW]] [[PLAN_V0]] [[WORKFLOW_GITHUB_DRIVEN]] [[GITHUB_SETUP]] [[README]]
