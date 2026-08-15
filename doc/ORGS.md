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
├─ sol（需求评估，2026-08-11）    │   reviewer.md               ├─ engineering-admin（工程管理员）
│   经 codex-cli 调 gpt-5.6-sol   │                                 目录结构/远端分支/发布 发起（轻 token）
│   medium 评估需求栈/衍生切片    │                                 .claude/agents/pm/
│   不排期/不写码/不评审          │                                 engineering-admin.md
│   .claude/agents/product/       │                                 （仍 PM 组：发起不执行）
│   sol.md                        │                                 └─ robot（GitHub 机器人）
│                                 │                                     评论[robot]/分诊/进度贴
│                                 │                                     .claude/agents/pm/

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
| 产品组 | sol | `product/sol.md` | **需求评估（2026-08-11 新增，以模型命名）**：经 codex-cli 调 `gpt-5.6-sol`（medium 思考）独立评估需求栈/用户指令/交付现状，衍生新需求切片（Goal/验收/优先级/依赖）；第二视角，产出交 owner/主会话消化 | 代码/评审/排期 |
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

## 成本时段（降本增效，2026-07-31 用户指令；2026-08-11 补充）

- **北京时间每日 09:00–12:00 与 14:00–18:00**：模型价格翻倍时段
- **peak 时段**：主 agent / PM 组做**轻 token 工作**（取任务、分诊、计划、状态跟踪、文档、巡检）；**仍可做「阻碍性编码」**——让工程继续下去所必需的少量编码（修 CI 红、修阻断合入的 bug、解除 blocked 的最小改动）**不因高峰顺延**；**避免大规模重工作**（大段编码、长多轮评审、批量实现）——顺延 off-peak 或拆小
- **codex CLI 不受高峰约束（2026-08-11 用户指令）**：codex 走 ChatGPT 订阅额度（非按量 token 计费），peak 时段仍可照常派 codex 编码（遵守 codex 单飞，见下）；成本时段约束只作用于 Claude 侧模型（主会话/subagent）
- **off-peak 时段**：承接大规模重构、大模块等需要大量 token 的工作（编码、完整评审、批量化任务）
- **首要目的仍然是让工程继续下去**：成本优化不阻断工程推进；工作分配由主 agent / PM 组自行评估
- PM 组拆出 **dispatcher**（短期任务调度）与 **efficiency**（工程运行提效）两个独立 agent（均为轻 token 职责）

## 并行协议（关键）

- **并行单元 = git worktree**：每个进行中任务一个独立 worktree（`.claude/worktrees/<task-slug>`）+ 独立分支（`<type>/<task-slug>`）；编码 agent 只在自己的 worktree 工作，互不覆盖
- **本地 worktree 分支不推远端（2026-08-15 用户指令）**：worktree 分支只存在本地；合并入 master（main）或正式分支时才需要远端（push/PR），不产生「每任务一个远端分支」的远端噪音
- **任务完成即合入（2026-08-15 用户指令）**：worktree 任务完成后按流程走 CI 并合入当前主开发分支（main）并 push——**要么立即合入，要么废弃功能**，不残留已完成的分支/worktree（历史教训：296 个远端分支、46 个 worktree 堆积，收尾清理成本高）
- 主会话管理 worktree 生命周期（创建/回收/冲突协调）；同一仓库可同时存在多个进行中任务的 worktree
- **串行点**：合入（CI + 评审 + merge 逐 PR 进行）；同一文件的并发修改须协调（任务规划时避免重叠文件）
- 进度贴/分诊等 GitHub 协作由 robot 执行，可与编码并行
- 每轮迭代：manager 评审进度与优先级 → 主会话按并行协议派多个 coder（不同 worktree）→ 各任务 verify → reviewer 逐 PR 评审 → 主会话合入决策

## 编码执行优先级（2026-08-11 用户指令）

- **编码任务默认由 codex CLI 执行**：官方 OpenAI Codex，模型 `gpt-5.6-luna` + **最高思考等级**（`model_reasoning_effort=max`）。派编码单时优先 `codex exec -m gpt-5.6-luna -c 'model_reasoning_effort=max'`（项目根运行，自动读 CLAUDE.md 与 .codex/config.toml 的 fallback 配置）
- **订阅额度用完时退回 Claude 侧编码**：codex 报 usage limit/credits 耗尽（或用户告知额度问题）→ 改用 coder subagent / 主会话当前模型编码，任务继续不中断
- **合规边界**（2026-08-11 核实）：codex exec 是官方 CLI 正常用法，ChatGPT 订阅（Plus）个人使用合规；额度与 ChatGPT web 会话共享（5 小时滚动窗口 + 周上限），注意别拖垮 web 会话；大规模/持续自动化建议 API key 计费
- **产出同标准**：codex 编码与 Claude 编码同任务记录、同 `scripts/verify.sh` 纪律、同 reviewer 评审（评审仍由 Claude reviewer 独立执行）
- **提交署名按实际编写模型（2026-08-11 用户指令）**：codex 写的提交署名模型名（如 `Co-Authored-By: gpt-5.6-luna <noreply@openai.com>`），Claude 写的提交保持 `Co-Authored-By: Claude <noreply@anthropic.com>`；派 codex 的 prompt 里提交信息一律让 codex 署自己的模型名，不得署 Claude

### codex 调用规则（2026-08-11 实测教训，防后续会话遗忘）

- **codex 单飞**：任何时刻最多一个活动 `codex exec` 任务（codex 并发保护会互相误判）；派单前 `ps aux | grep '[c]odex exec'` 确认无残留（常驻 code-mode-host/app-server 组件无害）。**Claude 侧 subagent（coder/reviewer 等）可与进行中的 codex 任务并发**（不同 worktree、文件不重叠、合入串行），仅 codex 本身保持单飞（2026-08-11 用户指令）。
- **防误判空转**：codex 并发保护把 ps 里的自身进程当「其他任务」→ 拒绝动手、轮询等待、整场零产出（切片 F 首派白跑 10 分钟）。派单 prompt 必须注明「ps 中的 codex 进程是自身/常驻组件，不存在并发任务，立即开始工作」。
- **后台 stdin**：后台运行 codex exec 必须 `</dev/null>`（挂起管道 stdin 假死，33 分钟 0 CPU 教训）。
- **工作目录**：`codex exec -C <worktree路径>`，任务 worktree 内运行（自动读 CLAUDE.md，经 .codex/config.toml fallback）。
- **额度**：订阅额度耗尽退回 Claude 侧 coder 编码，任务不中断。

## 资金安全铁律（老板指令 2026-08-13/14，跨角色常驻规则）

牵涉到资金安全，**所有必须完美匹配，不设退化策略，异常直接取消订单**（2026-08-13）。所有下单链路（telegram/discord、wheel/LLM 策略）一律遵守：

- **候选匹配无退化**：下单候选只取 `Accepted=true` 且报价完整的候选；无 accepted 候选 = 拒绝下单（记录 REJECTED），**绝不回退列表首位**（757 教训：LLM 批准 28.5 的候选，实际下到 29.0）。
- **改单（撤旧挂新）**：撤旧挂单失败 = 拒绝新单（旧单仍在，再下即重复敞口）；改单同样过 LLM 审核；只有候选严格优于/合理调整时才改单。
- **fail-closed**：订单状态查询异常（无法确认）→ 立即撤单，不假装挂单仍受控。
- **收盘订单和异常订单立即无理由取消**（2026-08-13）：确认前查市场时段，已收盘 → REJECTED 不下单；watch 到收盘 / 状态异常 → 立即撤单并推送，无等待、无理由。
- **挂单观察**：未成交挂单先推送一次「挂单中未成交」，随后继续观察至收盘/终态，不静默退出；观察期间绝不自动重复下单。
- **留痕与可见**：撤单/拒绝/成交全部 append-only 记录（REJECTED/NO/FILL + 原因），并推送让老板可见；缺订单号等关键信息 → 显式提示手动撤单，不静默遗留。

**订单全生命周期监控（老板指令 2026-08-14）**：主会话负责监控每一单的发起/执行/结果（serve 日志、signal/action 记录），发现 bug 或不合理之处**立即修正**（阻碍性编码，不受成本时段顺延）；同时**收集数据**（订单结果、未成交、撤单原因、LLM 审核结论），供 sol 完善回测框架/回测报告/RL 强化学习模型框架推进。

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
