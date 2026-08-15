# 回测工具链重构:ES 参数寻优 + CLI 化 + 未成交概率 + 参数通俗化

**State**: 计划已批准(2026-08-13 20:45,裁决书 `~/.claude/plans/mutable-nibbling-music.md`),切片 S0-S8 开始实施

## Goal

老板 2026-08-13 指令链:
1. 取消每日出手上限(下单时人工最终决定)
2. 回测工具链全面 CLI 化(agent 可直接调用),废弃 web 回测界面,结果直接推 Discord
3. 报告 RL 风格(迭代次数/收益/未成交率),HTML 移动端(iPhone 16 Pro Max 标准 430px)由标准数据直出(禁止 LLM 发挥)
4. 未成交概率按流动性启发式估算(期权没成交是常态)
5. 回测数据落策略缓存直接传给 LLM 动态策略帮助计算
6. 参数名全中文化(界面+报告),后端 JSON 键同步改,存量兼容
7. 回测往强化学习方向,后续替代人工指定参数

## Constraints(裁决)

- **技术栈 Go**(老板已定);阶段 1 = ES 参数寻优,不称策略级 RL
- **战术参数(6 维)RL 自动寻优;战略参数(满仓/清仓/最大持股)纯人工自定,不参与优化**(用户 2026-08-13 澄清)
- **路线:ES 先行 + 行为克隆预研**(用户拍板);深度 RL 为阶段 C/D backlog,见 doc/issues/draft-2026-08-13-deep-rl-eval.md
- 未成交概率启发式 `p_fail = clamp(0.05, 0.95, 0.55×(1-spread) + 0.30×(100/(vol+100)) + 0.15×(1000/(oi+1000)))`,model_kind=heuristic;只统计实际成交尝试
- 命名采纳 sol 方案:英文 snake_case 键 + 全中文展示;存量旧键自动映射(有损迁移标 migration_lossy=true)
- 缓存写入 ≠ 配置发布;所有回测/训练产物 RESEARCH_ONLY;永久链条「RL 提议 → Wheel 硬风控 → LLM 审核 → 人工决定」
- 每标的独立:参数集绑定 symbol+market+currency+config_version;范围/结果/报告不可跨标的复用
- 编码执行:codex 单飞优先(派单前 ps 确认),额度尽退 Claude coder;每片独立 worktree+分支、verify.sh 全绿、reviewer 评审

## Links

- 裁决书(完整计划): `~/.claude/plans/mutable-nibbling-music.md`
- sol 评估 1(命名/切片/未成交/ES 设计): doc/issues/draft-2026-08-13-backtest-rl-sol-eval.md
- sol 评估 2(深度 RL): doc/issues/draft-2026-08-13-deep-rl-eval.md
- 报告 JSON schema 定稿: doc/BACKTEST_REPORT.md
- 任务: #45(主) #46(S0) #47(S1) #48(S2) #49(S3) #50(S4) #51(S5) #52(S6) #53(S7) #54(S8)

## 切片表

| # | 切片 | 状态 | 验收产物 |
| --- | --- | --- | --- |
| S0 | 契约裁决 + 参数字典 + 报告 schema | DONE(2026-08-13) | 本任务记录 + doc/BACKTEST_REPORT.md |
| S1 | 参数后端与兼容迁移 | DONE(2026-08-13,merge 26445c4) | 新读旧只写新、每标的版本化、删日上限领域逻辑、契约+实库迁移测试 |
| S2 | 参数消费端中文化 | DONE(2026-08-13,merge b6a7ab5) | watchlist 配置入口/CLI 帮助/LLM 审核文本/文档一致 |
| S3 | 未成交模拟与基础指标 | DONE(2026-08-13,merge 3f5f781) | 订单假设/模型版本/稳定派生 seed/计数口径/多流动性等级测试 |
| S4 | 报告数据面 + 基础 CLI | DONE(2026-08-13,merge 3631710) | 单次回测输出版本化 JSON/HTML(430/390px),尚不接 Discord |
| S5 | ES 参数寻优 | DONE(2026-08-13,merge 1e6d4d9) | 搜索/约束/walk-forward/多 seed/早停/轨迹严格复用 S4 schema |
| S6 | 缓存与 LLM 上下文 | DONE(2026-08-14,merge 64f6554) | 带版本/状态缓存;过期不合格不注入 |
| S7 | Discord 推送 + Web 退役 | pending | 幂等推送/失败可重试/真实 channel smoke;CLI+报告链路验收后删 results |
| S8 | 行为克隆预研(并轨,RESEARCH_ONLY) | backlog | 规则+多组 ES 稳健候选做 teacher → HOLD+候选排序小模型;不提供收益证据 |

依赖顺序:S0 → S1 → (S2,S3) → S4 → S5 → (S6, S7);S8 并轨依赖 S5 轨迹数据面。Web 删除不得早于 CLI+HTML+Discord 链路验收。

## Discord 推送格式实测(2026-08-13 demo 验证,供 S7 使用)

- **正文链接可直开**:消息 content 里的链接 + 页面 og 元数据 → Discord 渲染预览卡(标题/描述/theme-color),点击直接打开,无确认
- **embed 字段内链接需确认**:embed description/字段里的外链点击触发「离开 Discord」确认页(平台侧行为,站长无法关闭)
- **结论格式**:正文 = 报告链接(带 og 预览);embed = 纯摘要(状态/收益/未成交率/参数,不放链接)。页面必须带 og:title/og:description/theme-color;Discord 抓取 UA 为 Discordbot/2.0 形态(域名层已放行)
- og:image 后续评估(需要图);HTML 附件无法在 Discord 内嵌渲染(只下载),PNG 摘要卡作为 S7 可选增强

## 端到端验收命令(全部切片合入后)

```bash
wbot backtest -symbol HK.00883 \
  -params '{"full_position_price":48,"zero_position_price":55,"max_inventory":22000}' \
  -train '{"move_interval_pct":["0.5","3"],"min_option_quality":["0.5","0.8"]}' \
  -report -push -cache
```

重复执行同 report ID 幂等。

## Next

- S1 派单(参数字典见裁决书,有损迁移名单:00883/09988 存量配置)
- 美股时段集群监视持续(Market closed; skipping 已观测)
