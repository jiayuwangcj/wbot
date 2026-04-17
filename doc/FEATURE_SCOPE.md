# 功能切片规模与可测性

约束：**每一块可合入的功能**都须小到能被 **自动化测试（或已约定的 CLI smoke）** 在本地与 CI 中验证；**不以对话或 LLM 输出作为验收依据**。

## 规模

- **一个功能切片** = 一次可独立 review 的 PR，通常对应 `doc/tasks/` 里的一条子任务或 Issue 中的一格最小步。
- 若无法写测试或无法在现有 CI 里表述验收（例如只有泛泛「优化体验」），**先拆小**（补 Issue/ROADMAP 条目标）再实现。
- 与 [[TDD_WORKFLOW]]、[[WORKFLOW]] 一致：Red → Green → Refactor；合入前本地至少 `scripts/verify.sh`（与 `ci.yml` 的 `test` job 对齐）。

## 验收 = test / 门禁

- **必须通过**：`go test`、`go vet`、以及 CI 中为该仓库配置的 `gofmt`、`-race`（Linux）、`staticcheck`、二进制 smoke（见 `.github/workflows/ci.yml`）。
- **禁止**：用「模型认为完成」替代上述门禁；评审意见可在人侧讨论，**合入门禁仍以仓库脚本为准**。

## 完成与提交

1. 实现与测试到位后：**commit**（建议小步、信息清晰的 message）。
2. **push** 触发 GitHub Actions；远程以 **workflow 全绿** 为验收闭环之一（见 [[AUTO_ADVANCE]]）。
3. **CI 报告**：由 Actions 内 **纯脚本** 写入 Summary（`ci-summary` job），**不调用 LLM**；见 [[GITHUB_SETUP]]、[[CI_REPORT]]。

关联：[[WORKFLOW_GITHUB_DRIVEN]] [[README]]
