# 00700 全历史重训重评 + 双口径报告(2026-08-15 老板拍板:「00700 全历史重训重评 + 报告并列双口径」)

## Goal

用 #86 修复(OTM 硬约束/正股卖出保护/covered_call_pct/双口径)+ #88 新参数(profit_take_pct/put-call_delta_max/min_iv_rank)对 00700 全历史窗口做 ES 训练重评,输出并列双口径(权利金口径 + 已实现口径)报告,回答「年化 99% vs 实际 20%」后的真实收益与可提升性。

## 前置(blocked by #88 合入)

- [x] #88 合入(2026-08-15 #341 → main 04b3aa8;本任务负责 #88 评审条件 4 义务勾销——平仓触发 + delta 过滤 + IV rank 生效,留证据)
- [x] 数据目标容器确定:回测 PG = **wbot-pg-ci-test**(127.0.0.1:5433→5432,bars 00700 全量 20,914 行已在;ingest 同一 DSN)
- [x] 数据回填:全量 option_quotes(2025-02-01..2026-08-13)已回填,**hkex 源 318,191 行**(≈ #67 验收 314,361 + 窗口前延 2025-02-01..02-09;bars 20,914 完好;snapshots 123,016;missing_days=0)

## State

- [x] 派单(2026-08-15 worktree .claude/worktrees/retrain-00700 分支 feat/retrain-00700 基线 04b3aa8;codex 额度尽 08-20 恢复 → Claude 侧 coder 执行)
- [x] 数据回填(2026-08-15 完成,318,191 行)
- [x] 冒烟(#88 评审条件 4 三证据,2026-08-15,见下)
- [x] 训练(两轮完成,双口径报告就绪,见下)
- [x] 判定:两轮均未跑赢可比基线 → **本次未产出可推荐参数**(不硬推),原因与证据见 reports/compare-00700-2026-08-15.md
- [ ] 结果汇报用户(#81 模式,待主管收口汇报)

### 训练结果(两轮,2026-08-15,报告 reports/bt-HK.00700-123-{60dfeae2,7d29ffa1}.json)

| | 第 1 轮 | 第 2 轮 |
| --- | --- | --- |
| 配置 | pop20/gen40/budget840/patience8 | pop24/gen60/budget2000/patience15 |
| 早停 | 10 代(valid 自 gen1 停滞) | 38 代(valid 自 gen22 停滞 0.01855) |
| 评估 | 225 | 965 |
| rank1 完整参数 | profit_take=0.7(上界)、put_delta_max=0.3530、call_delta_max=0.2504、min_iv_rank=0、covered_call=0.0118、dte 13/43 | profit_take=0.547、put_delta_max=0.3948、call_delta_max=0.3543、min_iv_rank=0、covered_call=0.0597、dte 11/39 |
| 样本外(5 seed 中位)premium/realized | **1.41%**(=1.41%,stock=0) | **1.67%**(=1.67%) |
| 年化 | 4.86% | 5.74% |
| max_dd | 3.06% | 6.03% |
| unfilled | 5% | 8.3%(2/24) |
| buy-hold 基线 | -6.82%,excess +8.24pp | -6.82%,excess +8.49pp |
| 费用 | 0 | 0 |

- **两轮结论收敛**:搜索充分(38 代/965 评估)后样本外仍在 1.4-1.7% 区间,**显著低于可比基线**(smoke-E premium 2.97%/realized 3.05%)→ 不是探索不足,是 #88 参数在 00700 上压缩收益:delta_max 过滤高权利金候选(premium_income 19,903 vs 基线 29,663,-33%)+ profit_take 平仓成本(close_cost 3,252 vs 0)
- 两轮均过拟合信号:train_best 持续涨(0.0453),valid_best 早停滞;valid 表现(1.86%)与 test(1.67%)同量级,不严重
- min_iv_rank 两轮最优均为 0(实盘护栏自动满足,无需人工钳制)

## 冒烟证据(#88 评审条件 4 勾销;test 窗口 2026-04-26..08-12,seed 123,cash 100 万)

报告:reports/smoke/bt-HK.00700-123-{0e8ba53a,bf8ee026,fb4b753c,6600c2df,ffb42f21}.json(原始在 /tmp/smoke-reports/,已复制固化)

| run | 参数增量 | attempts | premium_income | close_cost | premium_net | 结论 |
| --- | --- | --- | --- | --- | --- | --- |
| B(0e8ba53a) | 无过滤,仅 profit_take=0.5 | 20 | 25,484 | 4,443 | 21,041 | 平仓触发 |
| A(bf8ee026) | +delta_max=0.3 +ivrank=0.2 | 28 | 18,158 | 4,644 | 13,514 | delta 过滤生效 |
| C(fb4b753c) | +ivrank=0.2(无 delta_max) | 20 | 25,484 | 4,443 | 21,041 | =B,0.2 阈值未触发 |
| D(ffb42f21) | +ivrank=0.8(无 delta_max) | 22(1 unfilled) | 28,831 | 7,807 | 21,024 | ivrank 触发 mask |
| E(6600c2df) | 纯 #86 8 键(无新参数) | 28 | 29,663 | 0 | 29,663 | 可比基线 |

**证据 a 平仓触发 ✓**:五份报告 close_cost 均 >0(除 E 无 profit_take),attribution 恒等式成立(如 B: 25484−4443+1254.88=22295.88=realized)
**证据 b delta 过滤 ✓**:A vs C 唯一差异 delta_max=0.3,同数据量(319 批快照)决策链完全不同(28 vs 20 attempts,单笔权利金 1,274→649——高 delta 高权利金候选被滤)
**证据 c IV rank ✓**:ivrank>0 时引擎自动扩展 365 天参考窗口回放(75→319 批快照,代码路径激活);阈值 0.2 未触发(候选 rank 全 ≥0.2,与 B 决策链一致),阈值 0.8 触发(决策链改变 + 出现 unfilled)→ rank 过滤随阈值单调生效

## 重要发现(报告时必含)

1. **#86 基线报告(bt-HK.00700-123-7ecf5509.json)由旧代码生成:schema 1.4、result 无 realized 字段**,而当前基线 04b3aa8 代码 schema 1.5。#86 报告的 22.59%/年化 99.07% 与当前环境**不可直接对比**(旧代码行为差异:同参数同窗口复现只有 premium_net 2.97%,smoke-E)。当前环境可比基线 = smoke-E(8 键参数):premium 2.97% / realized 3.05%(close_cost=0,stock_realized +833)。
2. **代码 bug(不修复,只记录)**:`cmd/wbot/backtest_train.go` `tacticalParams()` 硬编码 9 键(move_interval_pct..max_dte),**丢弃 #88 的 profit_take_pct/put_delta_max/call_delta_max/min_iv_rank** → es_train 报告 candidates[].params 与 boundary_hits 失真(选中候选完整 13 键仍在 identity.config.params,部分缓解)。已核实训练与执行正确使用了 13 键(轨迹 mask_reason 显示 put_delta_max=0.3530 生效)。

## 双口径结论(2026-08-15,test 窗口 2026-04-26..08-12,seed 123,5 seed 样本外)

| 口径 | #86 报告(旧代码 schema1.4,不可比) | 可比基线 smoke-E(8 键) | 训练第 2 轮候选(13 键) |
| --- | --- | --- | --- |
| premium_net | 22.59%(年化 99.07%) | **2.97%**(年化 10.38%) | 1.67%(年化 5.74%) |
| realized | 12.02%(含股票 -10.57pp) | **3.05%**(stock +833) | 1.67%(stock=0) |
| max_dd | 4.56% | 6.55% | 6.03% |
| unfilled | 28% | 0% | 8.3% |
| close_cost | 0 | 0 | 3,252 |
| SELL_PUT/CALL | 23 次(全 put?) | 28 次 | 22/12(covered_call 0.0597) |

**回答「年化 99% vs 实际 20%」**:99.07% 是旧代码(schema 1.4,无 #86 修复)上的权利金口径,当前代码复现同一参数只有 2.97%——**旧数字不可比**。当前环境真实收益:基线 premium 2.97%/realized 3.05%;启用 #88 参数训练最优 1.67%(15 周窗口,年化 5.7-10.4%)。窗口 buy-hold -6.82%,策略超额 +8.5-8.9pp 稳定为正。**#88 增量贡献为负**:delta 过滤 + 平仓把权利金收入压低 33%、吃掉平仓成本,样本外收益降约 1.3pp——00700 高波动窗口下限制 delta 与提前平仓反而不利。

## 遗留问题(报告时必含)

1. 代码 bug:`cmd/wbot/backtest_train.go` `tacticalParams()` 硬编码 9 键,丢弃 #88 4 键 → es_train 报告 candidates[].params/boundary_hits 失真(identity.config.params 有完整键,缓解);需 reviewer 评估修 bug(裁剪键改为遍历搜索空间或全部 13 键)
2. #86 基线报告为 schema 1.4 旧代码产物,与当前不可比(本任务已用 smoke-E 补可比基线)
3. IV rank 数据限制:ingest 自 2025-02-01,训练起点 2025-02-10 的 rank 参考窗口仅 ~9 天(IVRankWindow=365d);训练期 min_iv_rank 最优为 0,此限制未实际影响结果,但更高 min_iv_rank 训练时需先回填 2024-02..2025-01 数据
4. 冒烟/训练报告均 fees=0(测试环境 fee 参数默认),费用拖累未计入

## 训练方案

- 全窗口 ES 训练,搜索空间含:covered_call_pct(#86)+ profit_take_pct/put_delta_max/call_delta_max/min_iv_rank(#88);战略参数(满仓/清仓/最大持股)人工给定不参与寻优
- walk-forward(train/valid/test 时间顺序,禁随机打散);训练 seed ≠ 测试 seed;样本外多窗口稳定胜出才输出候选
- 首跑先单窗口冒烟(平仓触发 + delta 过滤生效 + IV rank 分布),再全量训练(reviewer 3.8 建议)
- 多 seed 报告分布(≥5 成交 seed);报告版本化 JSON + HTML 直出 + 双口径并列

## 护栏

- 训练产出配置若含 min_iv_rank>0,部署实盘前需人工确认或钳制为 0(reviewer 3.6 护栏:实盘无 rank 数据源时 HOLD fail-closed,静默停摆是 money 系统最怕的)
- delta_max 实盘默认 0=不限制(#88 条件 1),训练产出参数部署时按训练值显式配置

## 收口(2026-08-15)

- [x] #88 条件 4 三证据勾销(冒烟 A-E,reports/smoke/)
- [x] 两轮训练完成(未产出可推荐参数,判定见上)
- [x] 双口径并列报告(reports/compare-00700-2026-08-15.md + 训练报告 JSON/HTML)
- [x] 9 键 bug 记录待修(不修,见遗留问题 1)
- [ ] 结果汇报用户(#81 模式,主管收口)

## Links

- 调研:doc/tasks/2026-08-15-wheel-optimization-research.md
- 修复:#86(doc/tasks/2026-08-15-backtest-dual-metric-fullwindow.md)
- 参数:#88(doc/tasks/2026-08-15-return-boost-params.md)
- 数据:#66 腾讯日 K 回填、#67 HKEX 期权日终回填
