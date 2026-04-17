# PLAN V0

目标：只做自动化，不做业务功能。

范围：

- 最小 Go 工程可测（`go test` / `go vet`）
- GitHub CI（Linux + macOS）
- TDD 标准流程文档化
- proposal 机制初始化

验收：

- 本地检查通过
- PR checks 全绿
- main 分支启用 auto-merge 规则

关联：[[WORKFLOW_GITHUB_DRIVEN]] [[TDD_WORKFLOW]] [[0001-automation-baseline]] [[README]]
