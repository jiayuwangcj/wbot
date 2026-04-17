# WORKFLOW GITHUB DRIVEN

统一入口：

- Feature / Plan / Bug / Release 都由 GitHub 留言触发
- 每个 PR 必须引用留言链接（comment URL）
- 留言之外的执行细节由 Agent 自动完成

执行约束：

- PR 描述包含 `Driven-By` 字段
- CI 校验 `Driven-By` 是否存在

完整工程流（含分诊、角色、标签）：[[WORKFLOW]]

关联：[[README]] [[PLAN_V0]] [[GITHUB_SETUP]] [[TDD_WORKFLOW]]
