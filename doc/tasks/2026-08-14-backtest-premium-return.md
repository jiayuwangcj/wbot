# 回测评价口径切换为权利金净收益 + 数据覆盖对齐(2026-08-14 老板指令)

## Goal

老板指令(2026-08-14,三条):①「仅成交21张,与实际不符」→ 期权成交太少;②「策略调整为以赚取权利金为主,仅仅急涨急跌时候应急操作」;③「忽略掉正股价差收益,仅以权利金为最大目标」。落地:
- **评价口径 = 权利金净收益(reward-3.0)**:权利金收入 − 期权平仓成本 − 期权/行权交割费用;正股已实现盈亏**不计入评价**(只保留审计)。
- **数据覆盖对齐**:期权快照最早 2025-02-10(实测 PG:HK.TCH* 121,510 行,lo=2025-02-10,hi=2026-08-14);此前 3 年(2022-03..2025-01)零快照 → OnBar DATA_BLOCKED 空转 HOLD,是「期权成交太少」根因。重训窗用 2025-02-10 起。
- 策略行为:卖期权是常态路径(现状已符合);正股仅急涨急跌应急(StockSuggestion 机制保留);本次报告无主动做T(正股 −15 万来自行权接股路径,33 次交割)。

## Constraints

- worktree: `.claude/worktrees/backtest-es-perf`(main=486baca,当前干净)
- 确定性铁律:同 seed 同 params 同数据 → 输出逐位一致
- attribution 恒等式(RealizedPnL = PremiumIncome − OptionCloseCost + StockRealizedPnL − Fees)保留不动;新加 PremiumNet 独立字段
- 报告 schema 版本推进(schema 1.3 → 1.4 或字段追加),CLI/accept 同步;代码署名 Claude(主会话实施,luna 额度已尽)
- 提交前 scripts/verify.sh 全绿;reviewer 评审后合入

## Links

- 前置任务:doc/tasks/2026-08-14-backtest-es-perf.md(评审合入 + reward-2.0 重训,报告 bt-HK.00700-123-7e5d56c3)
- 报告字段:bt-HK.00700-123-7e5d56c3.json(result/attribution/cost_model/trajectory)
- PG:wbot-pg-ci-test(192.168.215.2:5432,postgres/postgres,wbot_test)

## 诊断证据(2026-08-14 实测)

- trajectory 1090 步:步 0-720(2022-03..2025-01)全 HOLD 且 state_before.target=0(零值,未经 Evaluate)→ 期权快照缺失 DATA_BLOCKED;步 721+(2025-02-13 起)才出现 SELL_PUT/SELL_CALL(36+26)
- PG:option_quote_snapshots WHERE symbol LIKE 'HK.TCH%' → min(observed_at)=2025-02-10,max=2026-08-14,n=121,510,5136 合约;bars(HK.00700)2004-06-15..2026-08-14 全覆盖
- OnBar 适配器(strategy.go:411):CurrentPrice=batch.UnderlyingPrice(快照底层价,非日 K close);步 721 px=465 → target=948 与 batch 价 442(内插 1200×(1−0.21)=948)吻合 ✓
- 归因(旧报告):premium_income=290,246 / option_close=0 / stock_realized=−149,933 / fees=168(legacy 3×56)/ realized=140,145 → premium_net=290,078(29.01%)
- 本次训练 -fee 3 legacy;重训必须真实费用模型(-fee-option-per-contract 21 -fee-stock-per-lot 70)

## 实施

### A. 评价口径 = 权利金净收益(reward-3.0)

1. `internal/backtest/summary.go`:PnLAttribution 加 `PremiumNetAmount`(= PremiumIncome − OptionCloseCost − (OptionAmount + ExerciseDeliveryAmount));注释口径
2. `internal/backtestes/es.go`:RewardVersion = "reward-3.0"(权利金净收益主项,忽略正股)
3. `cmd/wbot/backtest_train.go` trainMetrics:NetReturn = r.Attribution.PremiumNetAmount / initialCash(注释更新)
4. `internal/backtestreport/report.go`:MoneyResult 加 premium_net_return_pct/amount;wheel 门控 net_return_* = 权利金口径;Attribution 透出 PremiumNetAmount;schema 1.4
5. `cmd/wbot/backtest_batch.go` CLI 输出加 `premium_net_return=`
6. `scripts/accept-backtest.sh` / `scripts/accept-backtest-report.sh` 同步

### B. 重训(数据覆盖对齐 + 真实费用 + 6 维搜索)

- 窗:2025-02-10..2026-08-13(期权数据覆盖)
- 费用:-fee-option-per-contract 21 -fee-stock-per-lot 70(真实,不用 -fee 3)
- 搜索:6 维战术参数(老板 2026-08-13 裁决全量):move_interval_pct/min_premium_per_share/stock_switch_pct/trade_gap/min_option_quality/min_dte~max_dte
- 战略参数:-params {full_position_price:400, zero_position_price:600, max_inventory:1200}(人工,不变)
- **6 维边界(2026-08-14 数据探针定标)**:HK.00700 put bid(DTE 5–45)P05=0.01/P25=0.39/P50=4.68/P75=24.23(15,336 行);DTE P10=8/P50=84/P90=304(全量);StockSwitchPct 语义=相对上次有效成交价变动≥阈值仅出正股建议(wheel.go:720-730),量级应高于 move_interval
  | 维 | 范围 | 依据 |
  | --- | --- | --- |
  | move_interval_pct | [0.005, 0.03] | 上次训练同区间,00700 日波动 ~1–2% |
  | min_premium_per_share | [0, 5] | put bid P05..P50 |
  | stock_switch_pct | [0.03, 0.15] | 急涨急跌 3%–15% |
  | trade_gap | [0, 200] | 股,离散;max_inventory 1200 的 0–17% |
  | min_option_quality | [0.5, 0.8] | 上次训练同区间 |
  | min_dte | [5, 30] | 离散;max_dte 固定 45(params 给)

## State

- [x] 诊断(快照覆盖/轨迹/归因/OnBar 价格源)
- [x] A 实施 + 单测/verify 全绿(verify.sh 全绿 2026-08-14)
- [x] reviewer 评审(2026-08-14 结论:feature,有条件合入)
- [x] P1-1 修复(HTML 标签随口径条件渲染)+ P2-1(premium-net 恒等式断言)+ P3-3(单测覆盖)
- [x] CI 修复(c5fee77):env WBOT_PG_DSN 与 -file 互斥误伤(main 预存红)+ main_test.go 三处 total_return 断言同步 realized/mark 口径(main 预存红);本地全套 + race + 真实 PG 全绿
- [x] CI 修复 2(b9c4bc5):TestBacktestExecuteIntegration restore 丢 NULL。根因:ingest futu_option_test.go 插 HK.00700 无 execution_status(NULL,4633c48 引入,main 预存)→ 完整 CI 命令(-p 1 ingest 先于 httpapi)必复现;restore 用 st.String 把 NULL 转 "" 违反 watchlist_execution_status_check(SQLSTATE 23514)。修:wlBinding 保留 sql.NullString 原样传参,NULL 恢复为 NULL。本地全新库(wbot_ci_probe)+ 长期库完整命令全绿
- [x] CI 修复 3(3d1640d):验收步骤 3/21 失败。① from_watchlist 503:ingest 测试残留 HK.00700(参数残缺无 full_position_price,b9c4bc5 restore 修复后原样恢复)→ from_watchlist 遍历失败 503。修:ingest 测试结尾自清理 DELETE。② CLI summary 形状:检查名已同步 reward-3.0 但 grep 模式仍 total_return + 漏 fees= 尾段。修:grep 改新字段 + 任意尾段。本地探测库全套 go test + 验收 21/21 全绿
- [x] CI 修复 4(本地,未推送):accept-wheel-live 13/24 → 24/24。均为预存验收脚本问题(CI 从未跑过它:accept-backtest 此前提前失败):① 市场时段门:wheelrun 按交易所墙钟跳过非交易时段评估,CI 20:58(美股盘前)全 symbol 跳过零信号 → 加验收专用逃生开关 WBOT_WHEEL_FORCE_MARKET_OPEN=1(wheel_scheduler.go 注入 deps.MarketOpen,生产永不设置)② 默认档检查过期:S1 重构后默认输出 full_position_price/zero_position_price,脚本仍 grep 旧 price/target_inventory → 改新格式 + trade_gap 显式 0(默认 gap 50 ≤ trade_gap 50 恒 HOLD 不出 ALERT)③ count==1 语义:LLM warning 打印 2 行/符号行 20s 窗口 2 次 → 改 ≥1。④ 修复后回归(6/24):fake option-quote 缺候选必需字段 bid/ask/vol/strike 与 quote 时间戳超时区(固定 UTC+8 格式化 vs 服务器 UTC 解析 → "quote is from the future")、funds 缺 required 字段 totalAssets/frozenCash 等 → 修 fake:update_time 按市场时区(NY/+08)格式化 + Funds 补全 7 个 required 字段。本地全绿 24/24
- [x] CI 修复 5(b01ed3d,已推送):6879130 的 waitFor 修时序但引入 data race——cond 直接读 len(fake.sends) vs ServeHTTP 另一 goroutine 写,CI race 必红(13:45 run 实锤,TestCallbackNoConfirmedCancelsOrder/TestCallbackNoCancelFailureTellsManual)。修:fakeTGServer 加锁内 sendCount(),两处 waitFor cond 改用;其余 len(fake.sends) 是 HTTP 同步调用返回后断言,无并发。本地 -race -count=5 全过
- [ ] PR #338 合入(CI 绿后;frontend dashboard hover 测试偶发 flaky,重跑后 pass)
- [x] B 重训 + 推送报告(2026-08-14 晚,报告 bt-HK.00700-123-7ecf5509,已推 Discord)

## 收口(2026-08-14 晚 B 重训完成)

- **踩坑记录 -cash 必须 1000000**:首次重训探针 0 成交拒绝训练(`valid_coverage=84.37% effective_trades=0`)。排查:默认 -cash 10000 撑不起 00700 卖 put 资金约束,候选全灭。对照 #80 报告(state_before.cash=1,000,000)确认 #80 用了 -cash 1000000(任务记录当时漏记,本次已补)。-cash 1000000 后探针 102 次成交 ✓。**命令固化:重训必带 -cash 1000000**。
- **数据异常发现(排期)**:bars(1d,fwd)窗口内 2026-07-30 起 close 突变为 2500-2800,与期权快照底层价(438-481)矛盾(~5.8 倍,疑似后复权/拆股因子污染);2025-02..2026-06 正常(411-677)。wheel 决策用 CurrentPrice=快照底层价不受影响(轨迹 px=441-481 全正常),且权利金口径不评估浮盈 → 本次报告可信;但 bars 本身需修(#66 回填链复权问题),排期。
- **B 重训结果**(窗 2025-02-10..2026-08-13,6 维搜索,population 20,budget 840,seed 123,费用 21/70):
  - ES 10 代 early_stop(225/840 评估),样本外窗口 2026-04-26..08-12(75 交易日)
  - rank1 候选:权利金净收益 **22.59%**(p10 20.4%/p90 24.1%),年化 99%,最大回撤 4.56%,未成交率 8.7%(候选多 seed 统计 28%)
  - 参数:move_interval_pct 0.5%(**撞搜索下界**,最优可能更低)、min_premium 1.53、stock_switch 14.5%、trade_gap 76、quality 0.53、min_dte 13、max_dte 45
  - 归因:权利金 227,572 − 正股已实现 −105,750(16 次行权接股,700 股持仓)− 费用 1,631 → 净 225,941
  - push_status=sent(已推 Discord);硬违规 0

## 评审结论(2026-08-14)

- reviewer:功能类型 feature;有条件合入。条件 P1-1:HTML「已实现盈利」标签与值(权利金口径)不符,须在重训推送前修复 → 已修复并合入
- P2 排期:① doc/BACKTEST_REPORT.md 补档 schema 1.4(net_return/premium_net 语义 + gross 随动)② strategy_cache payload 版本判别或 schema_version 入缓存 ③ 候选比较 vs_baseline 语义(权利金 vs buy-hold)文档注明
- P3 记录:main.go:768 多 symbol summary 行缺 premium_net_return;-cash 0 → NaN(默认 10000 无实害)

## Next

报告已推。遗留:① PR #338 合入(残留 docs/CI 修复 cherry-pick 后 rebase)② bars 复权异常修复排期 ③ move_interval 下界 0.005 撞边,可考虑扩展搜索下界重训。
