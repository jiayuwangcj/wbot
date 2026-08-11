# wbot 项目协作规则(自动加载)

wbot = futu 行情/模拟盘 + Wheel 策略实时提醒 + LLM 审核闸门 + Telegram 人工处置闭环。完整文档见 `doc/ORGS.md`(组织架构与并行协议)、`doc/WORKFLOW.md`(工程流程)、`doc/RELEASE_DAILY.md`(合批发布与部署)。

## 组织架构(五组九角色,详见 doc/ORGS.md)

| 组 | 角色(agent) | 职责 | 不碰 |
| --- | --- | --- | --- |
| 产品组 | owner | 需求衍生/切片/验收预期;无指令时自主生成任务;老板反馈唯一发贴权限 | 代码/评审/排期 |
| 产品组 | sol | 经 codex-cli 调 gpt-5.6-sol(medium)评估需求栈/衍生新需求切片 | 代码/评审/排期 |
| 开发组 | coder | 按任务记录实现、自测(scripts/verify.sh 等价)、独立分支提交 | 评审自己的活 |
| 开发组 | reviewer | 多角色评审(健壮性/容灾/API 兼容/产品体验/CI 覆盖/日志/粒度/调用方视角),**评审报告必须判定功能类型 feature/bugfix** | 修改代码 |
| PM 组 | manager | 进度评审、优先级调整、派单调度 | 代码/评审 |
| PM 组 | dispatcher | 取任务/任务记录/派单/verify 跟踪(轻 token) | 代码/评审 |
| PM 组 | efficiency | CI/工具链瓶颈评估提效建议 | 代码实施 |
| PM 组 | engineering-admin | 工程结构/远端分支/发布**发起**(不执行) | 执行 |
| PM 组 | robot | GitHub 评论([robot] 前缀)/分诊/进度贴 | 计划/代码 |
| 运维组 | operator | 版本发布(release.sh)、部署 ~/.wbot/releases/、巡检;阻碍提 [bug] issue | 编码/老板发帖 |

**主会话(Supervisor)职责收敛**:① 保持开发并行继续(worktree 分配/冲突协调/顺序合入)② 暴露系统性逻辑问题 ③ 合入决策(收评审报告决定合入/退回/有条件)④ 用户接口(用户在场时指令优先)。取任务/派单归 PM 组,需求衍生归 owner。

## 工程推进方法(并行协议,关键)

- **并行单元 = git worktree**:每个任务一个 `.claude/worktrees/<task-slug>` + 独立分支 `<type>/<task-slug>`;coder 只在分配的工作树工作,互不覆盖;主会话管理 worktree 生命周期。
- **任务记录**:每派单一记录 `doc/tasks/YYYY-MM-DD-<task-slug>.md`(Goal/Constraints/Links/State/Next),派单时写,收口时更新;子任务可恢复靠它。
- **一轮迭代闭环**:manager 评审进度与优先级 → 主会话按并行协议派多个 coder(不同 worktree)→ 各任务 verify → reviewer 逐候选评审 → 主会话合入决策 → 合入主基线。
- **串行点**:合入(评审 + merge 逐候选进行);同一文件的并发修改须协调(任务规划时避免重叠文件)。
- **评审独立**:coder 不评自己的活;reviewer 输出结构化报告(结论 + 功能类型 + P0/P1/P2/P3 发现),主会话按报告决策并把结论记入任务记录。
- **发布机制(2026-08-11 指令,废除日构建)**:reviewer 判定交付物 feature/bugfix;feature 在运维空闲时**合批发布**(正式版本 vX.Y.Z),bugfix 可及时发;始终保持可用;发布障碍由 operator 提 [bug] issue 开发修复后重发。

## 验收纪律(2026-08-02 用户规则)

- **本地全可用才提交/合 PR**:提交前 `scripts/verify.sh`(gofmt/test/vet/race/staticcheck/CLI smoke)全绿;关键改动在真实 PG(WBOT_PG_DSN)跑集成测;新能力挂验收脚本(scripts/accept-*.sh)并连跑通过。
- **逐端点验收**:HTTP/CLI 契约逐个端点验收,不留口头「应该可以」。
- **对账纪律**:ACCEPTANCE.md 的脚本数/检查项数逐脚本 `grep -c 'check "'` 实计、矩阵求和、总表自洽;运维沉淀进 scripts/ 与验收脚本。
- **敏感配置**:只允许 `~/.wbot/`(0600);仓库内禁止密钥/资产配置值;测试 fixture 用假值(见 doc/PRIVACY.md)。

## 成本时段(降本增效,2026-07-31 指令)

- 北京时间 09:00–12:00 与 14:00–18:00 模型价格翻倍:主会话/PM 组做轻 token 工作(取任务/分诊/计划/跟踪/文档/巡检);「阻碍性编码」(修 CI 红/阻断合入 bug/解除 blocked 的最小改动)不因高峰顺延;避免大规模重工作(大段编码/长多轮评审/批量实现)。
- 首要目的:让工程继续下去;成本优化不阻断工程推进。

## 项目关键约束

- **UI(前端 JS)永不调 `/v1/futu/quote`**(浏览器直连网关读限/鉴权问题),行情只经后端代理。
- 敏感目录 `~/.wbot/`(wbot.conf 运行时配置、config.yaml 部署级凭证、releases/ 产物);`.wbot` 不提交仓库。
- CI 纪律:禁用原生 `[skip ci]` 提交标记;文档/组织类改动走 ci.yml check-skip 自动检测。
- 详细契约:doc/API.md(HTTP)、doc/WHEEL_STRATEGY.md(wheel 产品边界:wheel-only、ALERT/HOLD 人工处置、DATA_BLOCKED capability)、doc/BACKTEST.md。

## codex 集成(2026-08-11)

- 用 codex 在本仓库开发时,项目指令直接读**本文件**:`.codex/config.toml` 配置了 `project_doc_fallback_filenames = ["CLAUDE.md"]`。codex 不支持 `@` include(AGENTS.md 内 `@path` 是惰性文本),每目录按 `AGENTS.override.md` → `AGENTS.md` → fallback 顺序只取第一个命中——因此本仓库**不设 AGENTS.md**,避免复制式内容漂移;新机器 clone 后 codex 即可读本文件,无需其他配置。
- codex 全局环境:审批规则白名单 `~/.codex/rules/default.rules`(verify.sh、docker 测试容器等已放行);`~/.codex/config.toml` model=gpt-5.6-sol、approval_policy=never、sandbox=danger-full-access、本仓库 trusted。
- **编码执行优先级(2026-08-11 用户指令)**:编码任务默认由 codex CLI 执行(`gpt-5.6-luna` + `model_reasoning_effort=max` 最高思考等级);ChatGPT 订阅额度用完时退回 Claude 侧当前模型编码;提交署名按实际编写模型(codex 写的署模型名,Claude 写的署 Claude);详见 doc/ORGS.md「编码执行优先级」。

## codex 调用规则(2026-08-11 实测教训,防后续会话遗忘)

- **并发控制是调用方职责(已固化 Claude 全局规则)**:单飞/串行派单/派单前 `ps aux | grep '[c]odex exec'` 确认、被拒即停——已写入 **`~/.claude/CLAUDE.md`**(所有 Claude 会话自动加载,跨项目)。codex 自己不会控制自己是否被并发调用,别把并发规则写进 codex 指令。
- **防误判空转(已固化 codex 全局指令)**:codex 并发保护把 ps 里的自身进程当「其他任务」→ 空转零产出(F 首派白跑 10 分钟)。修复已固化到 **`~/.codex/AGENTS.md`**(自身进程识别/立即开工/不嵌套,已验证 codex 自动加载生效),派单 prompt 无需手写。
- **后台 stdin**:后台运行 codex exec 必须 `</dev/null`(挂起管道 stdin 会假死,33 分钟 0 CPU 教训)。
- **工作目录**:`codex exec -C <worktree路径>`,在任务 worktree 内运行(自动读 CLAUDE.md,经 .codex/config.toml fallback)。
- **提交署名**:codex 写的提交署实际模型(`Co-Authored-By: gpt-5.6-luna <noreply@openai.com>`),Claude 写署 Claude。
- **额度**:订阅额度耗尽(codex 报 usage limit)退回 Claude 侧 coder 编码,任务不中断。
