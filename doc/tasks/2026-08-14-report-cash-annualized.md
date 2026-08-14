# 回测费用模型真实化 + 报告字段化(2026-08-14 老板指令)

## Goal

### A. 费用模型真实化(2026-08-14 老板指令,港股真实费率)

当前:`-fee N` 每笔成交固定扣 N 元,不区分资产类型与数量。改为真实费率:
1. **期权:21 HKD/张**——每次期权主动成交按合约张数计费,买入/卖出同费率(如卖 1 张 put = 21);roll 产生一笔平仓一笔开仓 = 21×2 = 42(老板举例确认 2026-08-14)
2. **正股:70 元/手**——每次正股成交按手数计费(00700 每手 100 股:买 500 股 = 5 手 × 70 = 350)
3. **主动购买和被动行权都需要计入成本(老板补充)**:主动买卖(期权/正股)按上述费率;被动行权/指派交割(期权到期 exercise/assignment 产生的正股交割)同样按正股手数费率计费(行权=按行权价成交正股),行权/指派事件本身计入成交记录与费用台账
4. **行权口径(老板确认 2026-08-14)**:期权行权本身**无费用**;行权/指派产生的正股交割按正股费率(70/手)计,不加期权费率——「平均 70 合理」
4. CLI:新增分类型费率 flag(如 `-fee-option-per-contract 21 -fee-stock-per-lot 70 -lot-size 100`),旧 `-fee` 单值保留向后兼容(两套费率同时给出时以分类型为准)
5. 报告 CostModel 结构化:区分 option/stock 费率与按张/手计费明细,TotalFees 含行权交割费
6. 重新跑基准回测,费用从 216(101 笔 × ~3 元)变为真实量级,报告随之变化

### B. 报告字段化(本金/期末权益/总收益/年化)

回测报告(JSON schema + HTML 投影 + Discord 展示)明确写出:
1. **本金**(initial_cash):固定以 100 万为基准写入报告(当前只在 trajectory[0].state_before.cash 隐含 1000000,顶层无字段)
2. **最后剩余**(final_equity_amount):terminal.final_equity_amount 已存在(如 996,320),提升为 result 层明确字段
3. **总收益**(net_return_amount + net_return_pct):result 已有(如 -3,680 / -0.37%),保持
4. **年化收益**(annualized_return_pct):**新增**——按数据窗口自然日天数折算:`(1+net_return_pct)^(365/days)-1`;窗口为 [from, to]
5. **总仓位以 100w 为标准**:持仓相关指标(持仓市值/期末权益/最大仓位)均以 initial_cash 为分母输出占比(如 stock_market_value_pct = 持仓市值 / 本金),金额与百分比并存
6. **损耗输出(老板补充)**:报告输出交易损耗 cost_drag——总费用金额(含主动成交 + 行权/指派交割费)、损耗占本金比例(cost_drag_pct = 总费用/本金)、损耗对收益率的拖累(cost_drag_return_pct),与毛收益/净收益并列展示(净 = 毛 - 损耗)

### C. wheel 策略新参数:最小期权收益 min_option_profit(2026-08-14 老板补充)

「预期收益小于 200 的期权基本毫无意义,都被损耗了」——增加策略参数:
1. 参数名:`min_option_profit`(暂定;单位 HKD/笔,与费率同货币)
2. 语义:候选期权单笔交易的**预期收益总额**(权利金 × 张数)低于该阈值 → 合约被过滤不交易;默认 200(老板举例「一般预期收益小于 200 基本毫无意义」)
3. 与现有 `min_premium_per_share`(每股权利金门槛)并存,两者都过才可交易;也入 ES 战术搜索空间
4. wheel 配置/验证/Evaluate 过滤链 + 报告 params 透传 + CLI 契约(默认值向后兼容)

## 位置

- internal/backtestreport/report.go:ReportConfig(45 行,加 InitialCash)、Result 结构(59-77 行,加 FinalEquityAmount/AnnualizedReturnPct)、BuildES/Build 入口(227 行 InitialCash 校验已有,写入 JSON 结构)
- internal/backtest/state.go:TerminalSummary 已含 FinalEquityAmount;年化计算需数据窗口天数(data_window.from/to,identity 已有)
- 两种报告都受益:固定参数 -report(全窗)与 ES -report(train 报告 result 为测试窗口)
- HTML 投影与 Discord embed 展示新增字段(projection 文件)

## 约束

- schema_version 语义:schema 1.1 → 1.2(新增字段;若评审要求向后兼容,新字段可选 omitempty 不破坏旧消费者;但用户明确要求报告可读,新增字段应必现)
- 年化口径:**自然日**(从 data_window.from 到 to 的秒数/86400);文档写明口径
- 总仓位基准:max_inventory × 市价 / initial_cash(占比),与「总仓位都以 100w 作为标准」一致
- 固定参数报告(全窗 372 bars,net_return 8.57%)与 ES 报告(测试窗口 -0.37%)各自独立计算,不混窗口
- verify 全绿;既有报告 schema 测试更新

## Links

- 报告:bt-HK.00700-42-9118f3a6.json(ES,total -3680)、bt-HK.00700-42-411c04e2(固定参数全窗)
- 任务记录:#70(前置,进行中)

## State

- [x] #70 已合入；本分支已 rebase 到主基线 `5cff3c3`
- [x] A: `FeeModel` 支持期权按张、正股/行权交割按手；CLI 分类型费率显式启用，旧 `-fee` 固定单值路径保留；`CostModel`/`FeeSummary` 记录分项台账
- [x] B: 报告 schema `1.2` 增加顶层 `initial_cash`、result 期末权益/年化/仓位占比、结构化 `cost_drag`；HTML 首屏、审计区和 Discord embed 均透传
- [x] C: Wheel 配置/验证/Evaluate 接入默认 `min_option_profit=200`，与 `min_premium_per_share` 串联过滤；报告 params、ES tactical search space 和实时候选存储均保留该字段
- [x] 确定性与实时路径回归：同 seed/params/data 的费用、报告 JSON/HTML 字节稳定；`wheelrun/wheelstore` 与全量 Go/race 测试通过
- [x] `scripts/verify.sh` 全绿；报告自包含验收 14/14

## Next

无代码收尾项；真实 PostgreSQL 全窗/ES 复跑需在设置 `WBOT_PG_DSN` 的数据环境执行。本 worktree 已完成自包含基准、验收和记录。

## Verification / benchmark（2026-08-14）

- `go test ./... -count=1`：通过。
- `npm test -- --run`：13 files / 76 tests 通过；`npm run build` 通过（仅保留既有 chunk size 与 `/ui/style.css` 构建提示）。
- `scripts/accept-backtest-report.sh`：14/14；`scripts/verify.sh`：`verify: ok`。
- 自包含 3-bar `HK.00700` buy-hold 基准（2024-01-01 100、2024-01-02 101、2025-01-01 102，`initial_cash=1,000,000`；本环境 `WBOT_PG_DSN` 未设置）：

  | 模式 | 期末权益 | 净收益率 | 年化收益率 | 总费用 | 损耗占本金 | 分项台账 |
  | --- | ---: | ---: | ---: | ---: | ---: | --- |
  | 分类型（21/张、70/手、100 股/手） | 1,013,000 | 1.300000% | 1.296425% | 7,000 | 0.700000% | 100 手正股 |
  | 旧 `-fee 3` | 1,019,997 | 1.999700% | 1.994182% | 3 | 0.000300% | legacy fixed/trade |

  分类型命令显式同时传入 `-fee 3` 与三项新 flag，验证新模型优先；买入 10,000 股按 100 手计费 7,000。相同 seed/params/data 重跑的 typed 报告保持同一 `report_id=bt-HK.00700-42-3ba5fbea`，JSON SHA-256=`5a5d664611cbec1ec0b06acbfc999afb101c6ff7b56625bcfe9b7756fd8d9a29`，HTML SHA-256=`6bd63eac7500ee97f14a6ad46b93dac6ba7ce87def3bb84416a674e4f755bd02`。
