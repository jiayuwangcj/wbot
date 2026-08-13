# S5 ES 参数寻优

**State**: 开发完成，待独立 reviewer（codex gpt-5.6-luna，2026-08-13）

## Goal

阶段 1 进化策略(ES)参数寻优:对**战术参数**(再次出手价差/最低每股权利金/正股切换阈值/免交易库存差/最低期权质量分/DTE 区间)自动寻优;**战略参数(满仓价/清仓价/最大持股)为人工自定常量,不进入搜索**(用户裁决)。产出 = 候选参数组 + 报告(复用 S4 schema,report_kind=es_train)+ 轨迹数据面(RL-ready)。

## 契约(裁决书 §三,唯一事实源 ~/.claude/plans/mutable-nibbling-music.md)

- **搜索空间 6 维,只含战术参数**: move_interval_pct / min_premium_per_share / stock_switch_pct / trade_gap / min_option_quality / min_dte~max_dte(约束参数化:先 min_dte 再非负跨度;整数独立离散步长)。**战略参数经 -params 人工给定,不参与训练**
- **范围按标的自身价格/合约乘数/历史分位数推导,禁跨标的复制**
- **超参首版**: 种群 20(16–24)、精英 20%、10% 随机移民、最多 30–50 代(总回测预算+超时控制,启动前输出预计评估次数)、连续变量归一化 [0,1] 初始 σ=宽度 10–15% 自适应、早停验证集 8–10 代相对改善 < 阈值(绝对+相对)、父代截断选择保留全局冠军、同 seed 同轨迹
- **奖励**: `score = 净收益率 − λdd×最大回撤 − λtail×尾部损失 − λturnover×成交成本`(归一化、版本化、权重入报告);硬失败(最大持股违规/裸Call/资金不足/未来泄漏)不软惩罚;未成交已体现于净收益,不重复计罚
- **验证纪律**: 时间顺序 train/valid/test 或 walk-forward(禁随机打散);训练 seed ≠ 测试 seed;验收单 seed 固定,产品报告 ≥5 成交 seed;样本外多窗口稳定胜出才输出候选,否则「无可推荐参数」
- **报告**: schema_version 复用 S4(doc/BACKTEST_REPORT.md),report_kind="es_train",含 identity/train(算法 ES/代数/种群/总评估次数/seed 列表/停止原因/耗时)/result(样本外净收益/基线超额/回撤/未成交率/费用滑点指派模型是否计入)/generations(每代轨迹)/candidates(前 3–5 组,多 seed 中位数/P10/P90)/audit(参数单位/搜索边界/撞边界标记/基线变化)/risk(RESEARCH_ONLY)
- **RL-ready 轨迹数据面**(冻结,供深度 RL 消费): 每步决策记录「决策前状态、完整候选集、每候选 mask 及原因、被选动作、模拟/真实 fill、下一状态、reward 原子分项、终止原因、配置/数据/代码/成交模型版本」;trajectory 落盘版本化,与报告同生命周期

## 改动面(不重叠区,先 grep 确认)

- **新包 internal/backtestes/**(~150 行纯 Go): ES 搜索/约束/早停/seed 派生
- **cmd/wbot/main.go runBacktest**: 新增 `-train '{"键":["min","max"]}'`(仅战术参数;范围 JSON 键名 = 参数字典 snake_case);输出报告复用 S4 报告管线(es_train kind)
- **internal/backtestreport/**: 扩展 schema 构造支持 es_train(generations/candidates 表)
- **奖励与评估**: 复用 internal/backtest 回测引擎做 fitness 评估(每个体一次 run;评估次数受预算控制)
- **验收脚本**: scripts/accept-backtest-train.sh(同 seed 同轨迹、最优个体 ≥ 种群均值、早停生效、walk-forward 无泄漏、-train 范围只含战术参数、战略参数不参与、启动输出预计评估次数)
- **文档**: doc/BACKTEST.md 补 -train;doc/BACKTEST_REPORT.md 补 es_train 字段(如 S4 已预留则只补说明)

## Constraints

- **不碰** S6(缓存/LLM)与 S7(Discord/Web)区域;不修改已冻结的 S4 single_run 字段(只扩展)
- 报告/训练产物一律 RESEARCH_ONLY,不写 watchlist/wheel_configs
- 训练 seed 派生确定性(复用 S3 run_seed 派生思路);同 seed 同轨迹必须可复现
- 时间预算:单次验证评估数受 -max-generations/-budget 显式控制,启动前打印预计评估次数
- 硬失败不软惩罚;撞边界标记入 audit;参数单位中文标注入报告

## Verify(验收)

- gofmt/vet/test/race/staticcheck + scripts/verify.sh 全绿
- 同 seed 两次训练:generations 轨迹一致、candidates 一致(确定性)
- 最优个体 score ≥ 种群均值(验收脚本断言)
- 早停生效:改善阈值内代数显著小于最大代数(脚本断言)
- walk-forward:禁随机打散(代码评审 + 测试断言时间顺序)
- 训练 seed ≠ 测试 seed(脚本断言)
- 独立分支提交,署名实际编写模型(codex 署 gpt-5.6-luna);CI 全绿
- 端到端(合入后): `wbot backtest -symbol HK.00883 -params '{"full_position_price":48,"zero_position_price":55,"max_inventory":22000}' -train '{"move_interval_pct":["0.5","3"],"min_option_quality":["0.5","0.8"]}' -report` 幂等可复算

## Links

- 主任务记录: doc/tasks/2026-08-13-backtest-toolchain.md(#51)
- 裁决书(唯一事实源): `~/.claude/plans/mutable-nibbling-music.md` §三
- 报告 schema: doc/BACKTEST_REPORT.md;S4 任务记录: doc/tasks/2026-08-13-s4-report-dataplane.md
- S3 任务记录(seed 派生复用): doc/tasks/2026-08-13-s3-unfilled-model.md

## Delivery

- 实现 `internal/backtestes`：战术参数白名单、DTE 约束参数化、离散步长、截断精英、随机移民、全局冠军、σ 自适应、预算/超时/早停及用途隔离 seed。
- `wbot backtest -train` 接入现有 DB 回测执行器，按时间 60/20/20 切分 train/valid/test；保留战略参数，封存测试使用 5 个独立 seed。
- schema 1.0 扩展 `es_train` 的 train/generations/candidates/audit/trajectory，训练产物固定 `RESEARCH_ONLY`；不稳定胜出时输出空候选。
- 新增 `scripts/accept-backtest-train.sh`（8 checks），并接入 `scripts/verify.sh` 与 CI。
- 验收：`scripts/accept-backtest-train.sh` 8/8、S4 报告回归 11/11、`scripts/verify.sh` 全绿（含 gofmt/test/vet/race/staticcheck/交叉编译/CLI smoke）。

**Next**: 独立 reviewer 按 feature 类型评审后，由主会话决定合入。
