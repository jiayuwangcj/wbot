# CI 报告（无 LLM）

本仓库在 GitHub Actions 工作流 **`ci`** 中的 **`ci-summary` job** 会向 **Workflow run** 的 **Summary** 面板写入一段 Markdown 报告。

## 原则

- **仅**由 workflow 内的 **shell / 内联脚本** 生成（`echo`、`heredoc` 等），**不**调用任何 LLM、不调用生成式 API。
- 内容**确定性**：便于审计与复现；指向本次 run 的 ref、commit、工作流链接。

## 与功能开发的关系

功能切片须 **可被同一套 CI 验证**（见 [[FEATURE_SCOPE]]）；Summary 只是 run 元信息与指向日志的入口，**不替代** `go test` 等门禁。

关联：[[GITHUB_SETUP]] [[WORKFLOW]] [[README]]
