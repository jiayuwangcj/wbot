# Doc Index

**Agent 根任务循环**（取任务 → Subagent 执行 → verify/CI → 更新 `doc/tasks` → 下一入口）：可读摘要见 [[AUTO_ADVANCE]]（内含 **任务来源**：Issue / Discussion / 长期目标，与 **取用优先级** ①～⑤）；机器与 Cursor 始终应用的完整规则见仓库根目录 `.cursor/rules/supervisor-subagent.mdc`。

入口与导航：

- [[WORKFLOW]]
- [[FEATURE_SCOPE]]（功能切片规模 · 验收必须可测 · 与 CI 的关系）
- [[SOURCE_TO_FEATURE]]（Issue/Discussion → 文档与功能的消化路径）
- [[AUTO_ADVANCE]]（根循环摘要 + 与 CI / 停机的说明）
- [[CRON_CONTINUE]]（外部 cron 守护续跑：cron-claude-continue.sh 用法与 /loop 关系）
- [[CI_REPORT]]（Actions Summary：无 LLM 的确定性报告）
- [[RELEASE_DAILY]]（日构建与本地部署：0 点后阻碍清零 → daily tag → 运维部署）
- [`doc/issues/`](issues/)（Issue/Discussion 待发草稿）
- [[tasks/README]]
- [[GITHUB_MCP]]
- [[WORKFLOW_GITHUB_DRIVEN]]
- [[ORGS]]（组织架构与并行协议：产品组/开发组/PM 组/运维组、角色分工、worktree 并行）
- [[PRIVACY]]（敏感配置与密钥安全：`~/.wbot/` 存放、不入库、评审必查）
- [[pinned_discussion]]（协作入口帖建议标题；发帖正文模板见 [[pinned_discussion_body]]，引用方式见 [[GITHUB_DISCUSSION_OPS]]）
- [[GITHUB_DISCUSSION_OPS]]
- [[ROADMAP]]
- [[DATA_PIPELINE]]（数据管道 v1：命令/行为/调度方式）
- [[DATA_STANDARD]]（数据标准：复权/source/时间基准/字段规范）
- [[FUTU]]（富途 OpenD 容器部署：凭证注入/启动/验证码/常见错误）
- [[ACCEPTANCE]]（验收体系总表：verify.sh / dev-up 冒烟 / 12 个 accept 脚本 + 覆盖矩阵）
- [[API]]（`wbot serve` HTTP 接口契约）
- [[BACKTEST]]（v2 回测骨架：命令/指标/约束）
- [[PLAN_V0]]
- [[TDD_WORKFLOW]]
- [[GITHUB_SETUP]]
- [[proposals/0001-automation-baseline]]
