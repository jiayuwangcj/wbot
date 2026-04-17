# WORKFLOW GITHUB DRIVEN

统一入口：

- Feature / Plan / Bug / Release 都由 GitHub 留言触发
- 每个 PR 必须引用留言链接（comment URL）
- 留言之外的执行细节由 Agent 自动完成

执行约束：

- PR 描述包含 `Driven-By` 字段
- CI 校验 `Driven-By` 是否存在
- **通用锚点评论（可选引用）**：`https://github.com/jiayuwangcj/wbot/issues/8#issuecomment-4268661869`（Feature Issue [#8](https://github.com/jiayuwangcj/wbot/issues/8)）；更细粒度需求可另开 Issue 并改用**该 Issue 下**的触发评论 URL。

完整工程流（含分诊、角色、标签）：[[WORKFLOW]]

关联：[[README]] [[PLAN_V0]] [[GITHUB_SETUP]] [[TDD_WORKFLOW]]
