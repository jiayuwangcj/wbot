# CI 报告（无 LLM）

本仓库在 GitHub Actions 工作流 **`ci`** 中的 **`ci-summary` job** 会向 **Workflow run** 的 **Summary** 面板写入一段 Markdown 报告。

## 原则

- **仅**由 workflow 内的 **shell / 内联脚本** 生成（`echo`、`heredoc` 等），**不**调用任何 LLM、不调用生成式 API。
- 内容**确定性**：便于审计与复现；指向本次 run 的 ref、commit、工作流链接。

## 与功能开发的关系

功能切片须 **可被同一套 CI 验证**（见 [[FEATURE_SCOPE]]）；Summary 只是 run 元信息与指向日志的入口，**不替代** `go test` 等门禁。

## CI Skip（文档/组织类 PR）

（2026-07-31 用户指令）各类组织架构/工程文档类、**不实际修改工程内容**的 PR 可以跳过 CI：

- **触发方式**：`check-skip` job 自动识别——PR 变更文件全部在 `doc/`、`.claude/agents/`、`.claude/rules/`、根级 `*.md` 白名单内
- **⚠️ 禁用原生 `[skip ci]` / `[ci skip]` 提交标记**：GitHub 原生机制会取消整个 workflow（连 check-skip 都不运行），required checks 永远等不到 check run，PR 挂起无法合入。跳过只能走自动路径识别
- **机制**：`check-skip` job 输出 `skip=true` 时，`test`/`db-integration` 的步骤级条件跳过实际工作但 job 报告 success——required checks（test/db-integration/governance）**不会挂起**；summary 面板标注 `CI skipped`
- **把关**：是否需要 CI 由 **reviewer 决定**（评审维度 5.9）——含工程内容变更却跳 CI 的 PR 会被 P1 阻断；ci.yml/工作流自身变更永远不跳过
- 本地验证（`scripts/verify.sh` 等）不因 CI skip 而省略，仍由 coder 自测

排查失败 job / step（命令行）：仓库内 **`scripts/ci_failure_detail.sh`**（读取 GitHub Actions API；可选设置 `GITHUB_TOKEN` 或 `GH_TOKEN`）。

关联：[[GITHUB_SETUP]] [[WORKFLOW]] [[README]] [[ORGS]]
