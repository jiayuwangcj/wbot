# TDD Workflow

流程：Red -> Green -> Refactor。

最低标准：

- Red：先写失败测试
- Green：最小实现通过测试
- Refactor：重构后测试仍全绿

**切片规模**：每个可合入功能须小到可被 **自动化测试或既有 CLI smoke** 覆盖；不得以口头描述代替，详见 [[FEATURE_SCOPE]]。

提交前：

- `go test ./... -count=1`
- `go vet ./...`
- `go test -race ./... -count=1`（建议）
- 与 CI 对齐时优先跑 `scripts/verify.sh`

关联：[[WORKFLOW]] [[PLAN_V0]] [[FEATURE_SCOPE]] [[WORKFLOW_GITHUB_DRIVEN]] [[GITHUB_SETUP]] [[CI_REPORT]] [[README]]
