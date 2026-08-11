# wbot 项目协作规则(codex 版,与 CLAUDE.md 对齐)

wbot = futu 行情/模拟盘 + Wheel 策略实时提醒 + LLM 审核闸门 + Telegram 人工处置闭环。完整文档:`doc/ORGS.md`(组织架构与并行协议)、`doc/WORKFLOW.md`(工程流程)、`doc/RELEASE_DAILY.md`(合批发布与部署)。

## 组织架构(五组九角色)

| 组 | 角色(agent) | 职责 | 不碰 |
| --- | --- | --- | --- |
| 产品组 | owner | 需求衍生/切片/验收预期;无指令时自主生成任务 | 代码/评审/排期 |
| 开发组 | coder | 按任务记录实现、自测、独立分支提交 | 评审自己的活 |
| 开发组 | reviewer | 多角色评审;报告必须判定功能类型 feature/bugfix | 修改代码 |
| PM 组 | manager/dispatcher/efficiency/engineering-admin/robot | 进度评审/派单/提效/工程治理/进度贴 | 代码实施 |
| 运维组 | operator | 版本发布(release.sh)、部署、巡检;阻碍提 [bug] issue | 编码/老板发帖 |

主会话(Supervisor)职责:① 保持并行继续(worktree 分配/冲突协调/顺序合入)② 暴露系统性逻辑问题 ③ 合入决策(收评审报告定合入/退回/有条件)④ 用户接口。

## 工程推进方法(并行协议)

- **并行单元 = git worktree**:每任务一个 `.claude/worktrees/<task-slug>` + 独立分支 `<type>/<task-slug>`;coder 只在自己 worktree 工作。
- **任务记录**:每派单一记录 `doc/tasks/YYYY-MM-DD-<task-slug>.md`(Goal/Constraints/Links/State/Next)。
- **一轮迭代闭环**:manager 排期 → 多 coder 并行(不同 worktree)→ 各任务 verify → reviewer 逐候选评审 → 主会话合入。
- **串行点**:合入(评审 + merge 逐候选进行);避免任务间重叠文件。
- **评审独立**:coder 不评自己的活;reviewer 输出结构化报告(结论 + 功能类型 + P0-P3 发现)。
- **发布机制(2026-08-11 指令,废除日构建)**:reviewer 判定 feature/bugfix;feature 运维空闲时**合批发布**(正式版本 vX.Y.Z),bugfix 可及时发;始终可用;发布障碍 operator 提 [bug] issue。

## 验收纪律(2026-08-02 用户规则)

- **本地全可用才提交/合 PR**:`scripts/verify.sh`(gofmt/test/vet/race/staticcheck/CLI smoke)全绿;关键改动真实 PG(WBOT_PG_DSN)集成测;新能力挂 scripts/accept-*.sh 验收并连跑通过。
- **逐端点验收**;对账纪律:ACCEPTANCE.md 脚本数/检查项逐脚本 `grep -c 'check "'` 实计、矩阵求和、总表自洽。
- **敏感配置**:只允许 `~/.wbot/`(0600);仓库禁密钥/资产值;测试 fixture 假值(doc/PRIVACY.md)。

## 成本时段

北京时间 09:00–12:00 与 14:00–18:00 模型价格翻倍:轻 token 工作为主;「阻碍性编码」(CI 红/阻断合入 bug/解除 blocked 最小改动)不因高峰顺延;避免大规模重工作。

## 项目关键约束

- UI 前端永不调 `/v1/futu/quote`(行情只经后端代理)。
- 敏感目录 `~/.wbot/`(wbot.conf/config.yaml/releases/);`.wbot` 不提交仓库。
- CI 禁用原生 `[skip ci]` 提交标记;文档类改动走 ci.yml check-skip 自动检测。
- 契约:doc/API.md(HTTP)、doc/WHEEL_STRATEGY.md(wheel 产品边界:wheel-only、ALERT/HOLD 人工处置、DATA_BLOCKED)、doc/BACKTEST.md。
- codex 注意:默认审批规则在 `~/.codex/rules/default.rules`(verify.sh、docker 测试容器等已放行);新会话若用 `codex` 在此仓库开发,遵循本文件与 doc/ORGS.md。
