# 00700 全历史重训重评 + 双口径报告(2026-08-15 老板拍板:「00700 全历史重训重评 + 报告并列双口径」)

## Goal

用 #86 修复(OTM 硬约束/正股卖出保护/covered_call_pct/双口径)+ #88 新参数(profit_take_pct/put-call_delta_max/min_iv_rank)对 00700 全历史窗口做 ES 训练重评,输出并列双口径(权利金口径 + 已实现口径)报告,回答「年化 99% vs 实际 20%」后的真实收益与可提升性。

## 前置(blocked by #88 合入)

- [ ] #88 合入(#88 评审条件 4:本任务负责真实 PG 00700 验证义务的勾销——平仓触发 + delta 过滤 + IV rank 生效,留证据)
- [ ] 数据回填:全量 option_quotes(314,361 行)不在现存 PG 容器(2026-08-15 实测 host/bridge/ci-test),重训前须重跑 `wbot ingest hkex` 回填;bars 00700 全量 20,914 行(2004→2026-08-14)在 wbot-pg-ci-test ✓
- [ ] 数据目标容器确定(回测用 PG,与 ingest 同一 DSN)

## 训练方案

- 全窗口 ES 训练,搜索空间含:covered_call_pct(#86)+ profit_take_pct/put_delta_max/call_delta_max/min_iv_rank(#88);战略参数(满仓/清仓/最大持股)人工给定不参与寻优
- walk-forward(train/valid/test 时间顺序,禁随机打散);训练 seed ≠ 测试 seed;样本外多窗口稳定胜出才输出候选
- 首跑先单窗口冒烟(平仓触发 + delta 过滤生效 + IV rank 分布),再全量训练(reviewer 3.8 建议)
- 多 seed 报告分布(≥5 成交 seed);报告版本化 JSON + HTML 直出 + 双口径并列

## 护栏

- 训练产出配置若含 min_iv_rank>0,部署实盘前需人工确认或钳制为 0(reviewer 3.6 护栏:实盘无 rank 数据源时 HOLD fail-closed,静默停摆是 money 系统最怕的)
- delta_max 实盘默认 0=不限制(#88 条件 1),训练产出参数部署时按训练值显式配置

## State

- [ ] 依赖 #88 合入 + ingest hkex 回填
- [ ] 训练 + 双口径报告
- [ ] 结果汇报用户(#81 模式)

## Links

- 调研:doc/tasks/2026-08-15-wheel-optimization-research.md
- 修复:#86(doc/tasks/2026-08-15-backtest-dual-metric-fullwindow.md)
- 参数:#88(doc/tasks/2026-08-15-return-boost-params.md)
- 数据:#66 腾讯日 K 回填、#67 HKEX 期权日终回填
