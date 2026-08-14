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
- [ ] reviewer 评审
- [ ] 合入 main
- [ ] B 重训 + 推送报告

## Next

提交分支 feat/backtest-premium-return → reviewer 评审 → 合入 → 重训推送。
