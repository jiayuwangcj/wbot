# 组织架构与并行协议

按常用软件公司分工的 Agent 组织结构（2026-07-31 用户确认）。**各角色互相必须可以并行**；目录结构按组分目录。

## 分组与角色

```
产品组 Product                   开发组 Dev                     PM 组 Program Mgmt
├─ owner（产品经理）             ├─ coder（编码组）            ├─ manager（项目经理）
│   需求衍生/切片/验收预期       │   实现/自测/独立分支提交    │   进度评审/优先级
│   自主任务生成（无指令时）     │   .claude/agents/dev/       ├─ dispatcher（短期任务调度）
│   老板反馈（唯一发贴权限）     ├─ reviewer（评审组）         │   取任务/派单/verify 跟踪（轻 token）
│   .claude/agents/product/      │   多角色评审（合入门禁）    ├─ efficiency（工程运行提效）
│   owner.md                     │   .claude/agents/dev/       │   CI/工具链瓶颈评估（轻 token）
│                                │   reviewer.md               ├─ engineering-admin（工程管理员）
│                                │                                 目录结构/远端分支/发布 发起（轻 token）
│                                │                                 .claude/agents/pm/
│                                │                                 engineering-admin.md
│                                │                                 （仍 PM 组：发起不执行）
│                                │                                 └─ robot（GitHub 机器人）
│                                │                                     评论[robot]/分诊/进度贴
│                                │                                     .claude/agents/pm/

运维组 Ops
└─ operator（运维）
    版本发布（release.sh）/部署/巡检
    不编码；阻碍→提 bug；老板事项→汇总产品组
    .claude/agents/ops/operator.md
```

## 角色职责一览

| 组 | 角色 | 文件 | 职责 | 不碰 |
| --- | --- | --- | --- | --- |
| 产品组 | owner | `product/owner.md` | 需求衍生、需求切片（Goal/验收/非目标）、验收预期、自主任务生成（无指令时）、**GitHub issue 与 ROADMAP 自主维护（含删除无用项）**、**老板反馈（唯一发贴权限，与主 agent 并列）** | 代码/评审/排期 |
| 开发组 | coder | `dev/coder.md` | 按任务记录实现、自测、独立分支提交 | 评审自己的活/计划 |
| 开发组 | reviewer | `dev/reviewer.md` | 多角色评审（健壮性/容灾/API 兼容/产品体验/CI 覆盖/日志/粒度/调用方视角） | 修改代码 |
| PM 组 | manager | `pm/manager.md` | 进度评审、三级优先级调整 | 代码/评审 |
| PM 组 | dispatcher | `pm/dispatcher.md` | **取任务/任务记录/派单/verify 跟踪**（轻 token，peak 时段主力） | 代码/评审 |
| PM 组 | efficiency | `pm/engineering-efficiency.md` | CI/验证/工具链瓶颈评估与提效建议（轻 token） | 代码实施 |
| PM 组 | engineering-admin | `pm/engineering-admin.md` | **工程结构管理（发起）**：目录树调整需求、远端分支管理/清理、发布发起（执行归 operator/robot/主会话） | 执行一切（编码/发版/清理 git） |
| PM 组 | robot | `pm/github-robot.md` | GitHub 评论（[robot]）、分诊、进度贴同步 | 计划决策/代码 |
| 运维组 | operator | `ops/operator.md` | 版本发布（release.sh；功能迭代运维空闲时**合批发布**）、部署到本地发布目录（~/.wbot/releases/）、部署/巡检、**发布阻碍提 bug**、老板事项汇总给产品组 | 编码/老板发帖 |

## 主会话（Supervisor）

责任**收敛**为：

1. **保持开发并行继续**——管理多任务并行（worktree 分配、冲突协调、顺序合入）
2. **暴露运行系统性逻辑问题**——跨任务/跨组的一致性、规则矛盾、流程断点；发现问题时改规则或上报用户
3. **合入决策**——收评审报告后决定合入/退回/有条件（0 approval，用户已授权自行评审）
4. 用户接口——用户指令的入口；用户在场时以用户指令优先

**已移交**：取任务/派单/verify → PM 组（manager）；需求衍生 → 产品组（owner）。

## 版本发布（合批，2026-08-11 用户指令；废除 2026-07-31 日构建机制）

- **废除日版本发布**（`daily-YYYYMMDD` tag 流程废止；历史日构建 tag/Release 保留为审计，清理归 engineering-admin）
- **功能类型判定（reviewer）**：评审报告必须判定交付物类型——
  - `bugfix`：修复缺陷、恢复既有契约/可用性，不引入新行为
  - `feature`（功能迭代）：新能力、行为变化、契约扩展
- **发布时机**：
  - `bugfix`：可及时发布（保持可用优先）
  - `feature`：**运维空闲时合批发布**——多个功能迭代合入主基线后攒批，由 PM 组/主会话与运维商定批次，一次发布（走正式语义化版本 `vX.Y.Z`，经 `scripts/release.sh publish --version vX.Y.Z`）
- **始终保持可用**：发布前 CI 绿 + 评审通过；发布不破坏可用性
- **发布障碍**：运维组（operator）提发布 bug 需求（GitHub issue，标题带 `[bug]`，正文含复现步骤/日志）→ 开发组修复 → 重新发布
- **运维组（operator）部署到本地发布目录**（约定 `~/.wbot/releases/`；部署步骤见 [[RELEASE_DAILY]]）

## 成本时段（降本增效，2026-07-31 用户指令）

- **北京时间每日 09:00–12:00 与 14:00–18:00**：模型价格翻倍时段
- **peak 时段**：主 agent / PM 组做**轻 token 工作**（取任务、分诊、计划、状态跟踪、文档、巡检）；**仍可做「阻碍性编码」**——让工程继续下去所必需的少量编码（修 CI 红、修阻断合入的 bug、解除 blocked 的最小改动）**不因高峰顺延**；**避免大规模重工作**（大段编码、长多轮评审、批量实现）——顺延 off-peak 或拆小
- **off-peak 时段**：承接大规模重构、大模块等需要大量 token 的工作（编码、完整评审、批量化任务）
- **首要目的仍然是让工程继续下去**：成本优化不阻断工程推进；工作分配由主 agent / PM 组自行评估
- PM 组拆出 **dispatcher**（短期任务调度）与 **efficiency**（工程运行提效）两个独立 agent（均为轻 token 职责）

## 并行协议（关键）

- **并行单元 = git worktree**：每个进行中任务一个独立 worktree（`.claude/worktrees/<task-slug>`）+ 独立分支（`<type>/<task-slug>`）；编码 agent 只在自己的 worktree 工作，互不覆盖
- 主会话管理 worktree 生命周期（创建/回收/冲突协调）；同一仓库可同时存在多个进行中任务的 worktree
- **串行点**：合入（CI + 评审 + merge 逐 PR 进行）；同一文件的并发修改须协调（任务规划时避免重叠文件）
- 进度贴/分诊等 GitHub 协作由 robot 执行，可与编码并行
- 每轮迭代：manager 评审进度与优先级 → 主会话按并行协议派多个 coder（不同 worktree）→ 各任务 verify → reviewer 逐 PR 评审 → 主会话合入决策

## 目录结构（适合并行）

```
.claude/agents/          # 角色定义按组（product/ dev/ pm/）
.claude/worktrees/       # 并行任务 worktree（git worktree add）
doc/tasks/               # 任务记录（每任务一条，并行任务的记录互不依赖）
doc/issues/              # 需求切片草稿（owner 产出）
tools/                   # 可复用工具脚本（python/shell，新写脚本统一入此；scripts/ 保留 CI/发布链路脚本）
doc/ORGS.md              # 本文档
```

关联：[[AUTO_ADVANCE]] [[WORKFLOW_GITHUB_DRIVEN]] [[README]]
