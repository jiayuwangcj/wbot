# 00700 回测正确性：DTE 窗放开 + 周末 stale 豁免（2026-08-15 老板指令）

## Goal

拉取 00700 日内线（数据量暴增）后，全窗口回测零成交空转（wbot 会话 2026-08-15 定位，见 Links）。修两个根源，让回测在**小数据量**上产出真实成交：

1. **DTE 窗硬锁 [5,10]**：港股期权月到期为主，[5,10] 大部分时间无候选 → 零成交
2. **周末 stale**：hkex 日终快照 16:00 HKT + `max_quote_age_seconds` 默认 86400(24h) → 周一 bar 用上周五 70h 快照，全 DATA_BLOCKED（实测 6/8、6/15、6/22、6/29）

## 改动面

### A. DTE 硬锁 4 处 → 放开到 45（引入 `MaxWheelDTE = 45` 常量，避免魔法数字）

| 位置 | 现状 | 改法 |
| --- | --- | --- |
| `internal/wheel/wheel.go:156` Config.Validate | `if c.MinDTE < 5 \|\| c.MaxDTE > 10 \|\| ...` | `MaxDTE > MaxWheelDTE` |
| `internal/strategy/strategy.go:70` wheel 模板 min_dte | `Max: 10` | `Max: MaxWheelDTE` |
| `internal/strategy/strategy.go:71` wheel 模板 max_dte | `Max: 10` | `Max: MaxWheelDTE` |
| `internal/backtestes/es.go:81` ES 搜索空间 | `(lo < 5 \|\| hi > 10)` | `hi > MaxWheelDTE` |

### B. 周末 stale 豁免

`internal/wheel/wheel.go:439` 当前 `asOf.Sub(qt) > cfg.QuoteMaxAge()` 报 stale。日终快照语义 =「该交易日收盘时的期权状态」，对**下一交易日**的盘中 bar 有效，应按交易日而非自然小时判断新鲜度。

- 方向：跨非交易日豁免——当 `asOf` 是 `qt` 所在交易日的下一交易日（之间无非交易日）时，不算 stale
- 复用 `internal/datacheck/calendar.go` 的 `ExchangeCalendar.Session(symbol, date).TradingDay`（offline、无网络）；或 adapter 层（`internal/backtest` 读快照处）归一化日终快照时间
- 由实施者设计，验收以「周一 bar 不再 DATA_BLOCKED」为准

## Constraints

- worktree: `.claude/worktrees/00700-dte-stale-fix`（分支 `fix/00700-dte-stale-fix`，基于 main=004329b）
- **确定性铁律**：同 seed 同 params 同数据 → 输出逐位一致；改动只影响候选筛选/新鲜度判断，不改变成交/收益计算路径
- **不污染实时路径**：wheelrun 实时评估（每 5 分钟单信号）不受影响——stale 豁免只针对回测日终快照场景，实盘走 `wheelrun` 不走 `wheel.Evaluate`
- 提交前 `scripts/verify.sh` 全绿
- 编码：codex 欠费(usage limit,2026-08-15) → 退回 Claude 侧 coder；提交署名 `Co-Authored-By: Claude <noreply@anthropic.com>`
- **验收**：短窗口（如 2026-06 单月）回测 00700 产出真实成交（attempt>0/fill>0），周一 bar 不再全 DATA_BLOCKED

## Links

- wbot 会话定位的缺口：`doc/tasks/2026-08-15-intraday-bars-fill.md`（「数据链路缺口」三则）
- 实盘 30 天 clamp：`bd7f71e fix(wheelrun): option chain 30 天 clamp 补合`
- ES DTE 45 排期：`doc/tasks/2026-08-14-backtest-es-perf.md`（P2「hkexHistoricalOptionCycleComplete 闸门按 DTE 10」）
- 复用：`internal/datacheck/calendar.go`（ExchangeCalendar.Session TradingDay）、`internal/wheelrun/market_hours.go`

## State

- [x] A: DTE 4 处放开（main 已有 `b83a04c`，非本次改动面）
- [x] B: 周末 stale 豁免（coder `29171fe`，TradingCalendar 注入）
- [x] 短窗口回测 00700 验证真实成交（2026-06 单月，cash 100万：attempt=13/fill=12/unfilled=7.69%，realized +1.61%，年化 +23.1%，超额 +12.36% vs buy-hold −10.76%）
- [x] verify 全绿（coder 已跑）
- [x] reviewer 评审（bugfix，有条件合入，无 P0/P1）+ 合入 main（29171fe）

## Reviewer 评审结论（2026-08-15）

- 结论：**有条件合入，无 P0/P1**；功能类型 **bugfix**（修周末 stale 误判致回测零成交）
- **P2 排期（不阻塞）**：
  1. `backtestexec.go` `quoteRangeStart` 加载窗口未随交易日豁免扩展——窗口**首根 bar** 为周一/长假后仍 DATA_BLOCKED（前一交易日快照被 `since=from-24h` 排除）
  2. `wheel.go` `QuoteFresh` 豁免未限定「日终快照」——可误豁免盘中陈旧快照（建议收紧为 qt≥ready 时间，或文档化边界）
  3. `datacheck` 日历 2026-only——2015–2025 周一假日周仍 DATA_BLOCKED（全窗口回测自 2015 的真实缺口）
- **P3 排期**：`nextTradingDay` 14 天魔数抽常量；补长假正例/跨多日陈旧负例/14 天上限边界测试；`doc/BACKTEST.md` freshness 语义同步（回测离线路径按交易日、实时路径墙钟）

## 验证记录（2026-08-15）

- 命令：`wbot backtest -dsn <wbot_test> -symbol HK.00700 -strategy wheel -timeframe 1d -adjust fwd -from 2026-06-01 -to 2026-06-30 -cash 1000000 -params '{full_position_price:400,zero_position_price:550,max_inventory:1200,min_dte:5,max_dte:45,min_option_quality:0,min_option_profit:0,min_premium_per_share:0,trade_gap:0}'`
- 结果：attempt=13 / fill=12 / unfilled=1（7.69%）；realized +1.61%、年化 +23.1%、超额 +12.36%（vs buy-hold −10.76%）；5 次行权、4 个未平仓期权腿
- **关键教训**：零成交第三根因是 `-cash` 默认 10000 太小——卖 1 张 put 的行权义务（strike×100）即超现金，候选全被「cash/margin is insufficient for put assignment」拒掉；真实回测须显式 `-cash 1000000`

## Next

任务 2（profile 优化方案）+ 任务 3（Discord 推送报告字段丰富）
