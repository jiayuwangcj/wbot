# WORKFLOW

本仓库工程流程对齐「**一切以 GitHub 上的可追溯留言为源头**」；结构上参考 Kubernetes 等成熟开源项目（**异步分诊、标签语义、单一事实来源在仓库**），但压缩为 **个人维护 + Agent 执行** 的版本。

## 参考（外部）

- Issue 分诊思路：[kubernetes/community — Issue Triage](https://github.com/kubernetes/community/blob/master/contributors/guide/issue-triage.md)
- 增强提案体系（类比 KEP）：本仓库用轻量 `doc/proposals/` 代替完整 KEP 流程

## 原则（与 k8s 的对应关系）

| 概念 | Kubernetes 常见做法 | wbot 落地 |
| --- | --- | --- |
| 单一入口 | Issue / KEP / SIG 讨论 | Issue / Discussion **留言 URL** 作为触发源 |
| 分诊 | `needs-triage`、SIG、优先级 | 用标签 + 评论完成「是否受理、缺什么信息」 |
| 所有权 | `sig/*`、`area/*` | 用 `area/*`（模块）即可；不设 SIG |
| 可执行变更 | PR + review + CI | PR + **Driven-By** + CI + **TDD** |
| 异步 | 以评论驱动，减少同步会议 | 以 **GitHub 评论** 驱动，Agent 补执行细节 |

## 端到端生命周期

1. **意图**：在相关 Issue / Discussion / PR 评论里写清目标（可引用 [[WORKFLOW_GITHUB_DRIVEN]]）。
2. **登记**：必要时开 Issue（模板已带 `Trigger comment`），把 **留言链接** 贴在描述里。
3. **分诊**：维护者打标签、补信息；未就绪则保持「需信息」状态，不开始写代码。
4. **设计**：较大改动先写 `doc/proposals/NNNN-*.md`，再链接回 Issue。
5. **交付**：分支 → 测试先行（[[TDD_WORKFLOW]]）→ PR 填 **Driven-By** → CI 绿 → 合入。
6. **发布**：用 Release 模板登记；版本说明指向对应 Issue/Discussion。

## 角色（缩小版「SIG」）

- **Owner**：你本人，做分诊、拍板、合并、对外部账号与密钥负责。
- **Agent**：按评论与仓库规则自动改代码、跑检查；不替代 Owner 做产品决策。
- **CI**：门禁（测试、vet、治理检查），见 [[GITHUB_SETUP]]。

## 标签约定（建议）

在 GitHub 仓库中逐步启用（可与现有 `feature` / `bug` 等并存）：

| 前缀 | 含义 | 示例 |
| --- | --- | --- |
| `kind/*` | 类型 | `kind/bug`、`kind/feature`、`kind/chore` |
| `area/*` | 模块 | `area/agent`、`area/mcp`、`area/data`、`area/web` |
| `triage/*` | 分诊 | `triage/needs-information`、`triage/accepted` |

新建 Issue 可继续用模板自带标签；分诊时再补 `kind/*` / `area/*`。

## 与现有机制的关系

- **留言驱动**：[[WORKFLOW_GITHUB_DRIVEN]]
- **测试纪律**：[[TDD_WORKFLOW]]
- **路线图**：[[ROADMAP]] 与仓库内 roadmap issue
- **协作入口帖**：[[pinned_discussion]]

关联：[[README]] [[PLAN_V0]] [[0001-automation-baseline]]
