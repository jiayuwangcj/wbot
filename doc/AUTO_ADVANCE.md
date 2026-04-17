# 自动推进（Agent 默认）

当主对话**没有给出具体目标**时，仓库约定 Agent **不得空转结束**，应从「计划」中取出**下一条可执行最小步**并推进闭环。监督细节写在仓库根目录 `.cursor/rules/supervisor-subagent.mdc`（与本文同步）。

## 计划里包含什么

除 `doc/PLAN_V0.md`、`doc/ROADMAP.md` 与 `doc/tasks/` 外，**GitHub Issue / Discussion 中已分诊并整理进仓库的需求**同样视为计划来源，例如：

- Issue / Discussion 正文中明确的「目标 / 验收 / 计划条目」；
- 或摘录/链回到 `doc/tasks/*.md`、`doc/proposals/*.md`，并在 Issue / Discussion 里用一句话指回文件路径。

这样 Issue / Discussion 的结论是**单一事实来源**在仓库（可复制恢复），Agent 轮次之间不会依赖口头记忆。

## 优先级（摘要）

完整顺序以 `.cursor/rules/supervisor-subagent.mdc` 为准；原则是先推进**已在 tasks 中排队/进行**的任务，再考虑 Issue/Discussion 中已落账的需求，再对照 v0 验收与路线图。

## 与 CI 的关系

- PR 仍须满足 `Driven-By` 等门禁，见 [[WORKFLOW_GITHUB_DRIVEN]]。
- 分支保护与必需 check，见 [[GITHUB_SETUP]]。
- **小步闭环**：每完成一个可合并小步，应先**本地验证**（至少 `scripts/verify.sh`，或与 `.github/workflows/ci.yml` 中 `test` job 等价），再 **commit / push** 以触发 Actions；把 **CI 绿色**当作该小步的远程验收。详见 `.cursor/rules/supervisor-subagent.mdc` 中「小步提交、CI 验证与等待用户」。
- **等人 vs 继续**：推送后可停下来与用户交互；若 **workflow 已结束**且用户**未**再给新指令，Agent 应按同一规则文档从计划里**继续下一小步**，不以「干等回复」为唯一终点。

## 停机与复盘（自动化要迭代的部分）

若本轮**必须停下**（无法在仓库内继续推进），主会话应**简短记录**并尽量**改一条规则或文档**，避免下次在同一处卡住：

| 常见原因 | 改进动作示例 |
| --- | --- |
| 无法连接 GitHub | **优先**：确认 Cursor [[GITHUB_MCP]] 可用并用 MCP 发帖/拉取；**其次**：安装 `gh` 并 `gh auth login`；**再其次**：把草稿放在 `doc/issues/` 人工粘贴，或按 [[GITHUB_DISCUSSION_OPS]] 用 `curl` + `GITHUB_TOKEN` |
| 目标含糊 | 把验收写进 `doc/tasks/` 或 Issue 正文，再启动 Subagent |
| CI / 格式 / staticcheck 失败 | 本地先跑与 CI 同等的检查；必要时在 `doc/GITHUB_SETUP.md` 注明必需 job 名称变化 |

**原则**：停机原因要能从 **git 里的某次提交** 或 **`doc/tasks` 某条记录** 追到，而不是只留在聊天里。

关联：[[WORKFLOW]] [[tasks/README]] [[README]]
