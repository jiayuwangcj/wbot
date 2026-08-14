# wheel 策略收益率优化调研(2026-08-15 老板目标:「回测并仔细检查正确性,搜搜方案提高收益率完善 wheel 策略」)

## 调研结论(行业标准 vs 我们现状)

| 维度 | 行业标准 | 我们现状(bt-7ecf5509 实测) | 差距 |
| --- | --- | --- | --- |
| 卖出方向 | OTM 25-30Δ put(72% 到期价外) | **全 ITM**(delta≈0.8-1.0,行权率 94%) | 致命,已派修(#86) |
| 退出规则 | **50% max profit 平仓或 21 DTE** | 17 次全持有到期(close_cost=0) | **最大提升点** |
| 滚动 | 跌向 strike roll down-out / 涨破 roll up-out,净贷方 | 无 | 阶段 2 |
| IV 过滤 | IV Rank > 35、VRP 为正才卖 | 无 | 数据已有(官方 IV) |
| DTE | put 30-45 / call 21-30 | min 13..max 45 训练范围 | 部分对齐 |
| covered call | 行权价 ≥ cost basis、0.2-0.3Δ | 卖 ITM(行权价 < 市价) | 已派修+参数化 |
| 行权率 | ~28%(25Δ) | 94% | 修复后应显著降 |

## 关键量化证据(SPY 5 年回测,ApexVol 2026)

**put delta 甜点 25-30Δ**(10% max profit 平仓 + 21 DTE 退出管理下):

| put delta | 胜率 | 行权率 | 5 年净收益 |
| --- | --- | --- | --- |
| 10Δ | 89% | ~11% | +18% |
| 16Δ | 82% | ~18% | +31% |
| **25Δ** | **72%** | **~28%** | **+41%** |
| 30Δ | 66% | ~36% | +39% |
| 40Δ | 55% | ~52% | +22% |

- 低 delta 泄收益率(权利金太少),高 delta 加速行权(call 腿更频繁,封顶上涨)——25Δ 是不动点
- **50% 利润平仓**:theta 收割策略回测中稳定跑赢持有到期(condor/spread 深潜 4-5× net P&L);QuantWheel:错过 50% 目标每例年损失 ~$3,600
- covered call 铁律:**行权价永不低于成本价**(「never cap yourself below breakeven」);蓝领投资者方法论:OTM call = 进取(涨势捕获多、权利金少),ITM call = 防御(收益高、下跌保护强)——**与我们老板拍板一致**
- wheel vs buy-hold 边界:低波动防守股**赢**(KO +27% vs +18%)、宽基**平且回撤减半**(SPY +41% vs +58%,回撤 −13% vs −22%)、上涨单票**输**(AAPL +62% vs +128%)——call 腿封顶是跑输来源;2022 熊市 wheel −2% vs buy-hold −19%(抗跌 17 点)
- 股票选择:流动性(spread < 0.10-0.15、OI 1000+ 手/日)、IV 25-50%、无 earnings 在 DTE 窗口、盈利基本面
- 仓位:单仓抗 −5%/−10% 压力测试,按年化 ROIC 与安全边际定仓

## 落地计划(修复 #86 合入后,00700 全历史重训前/中实施)

1. **50% 利润平仓参数 `profit_take_pct`(第一优先,收益率最大提升点)**
   - 回测引擎当前只有持有到期+行权两种退出(terminal.close_cost 恒 0),需支持中途平仓:权利金回落到 max_profit×50% 时买回平仓,释放保证金+资金周转
   - 进 ES 搜索空间(如 [0.3, 0.8],0 = 持有到期)
   - 实盘 wheelrun 同步:信号候选按该规则给出平仓建议
2. **delta 目标区间参数**(如 `put_delta_max=0.30` / `call_delta_max=0.30`,或单 `target_delta` 带容差)
   - 候选过滤/排序用 delta 而非纯权利金(mid/strike×10 的 premium 分偏 ITM);OTM 硬约束之上再加 delta 上限,双保险
   - 进 ES 搜索空间
3. **IV rank 过滤 `min_iv_rank`**(数据面已有 RP006 官方 IV,可算历史百分位)
   - 低 IV 时段跳过卖出(或拉宽 strike/延长 DTE),ES 训练
4. **滚动机制**(阶段 2):跌向 strike 时 roll down-and-out(net credit,否则接受行权);涨破 strike 时 roll up-and-out——引擎改动大,排期
5. **00700 全历史重训重评**(#87):上述参数(covered_call_pct + profit_take_pct + delta 区间 + IV rank)一起进搜索空间,报告双口径

## 正确性检查清单(「仔细检查正确性」,重训时逐项核对)

- [ ] 行权逻辑:assignment 价格/数量、正股成本加权(接股均价 562.5 已核对:14 张 ITM put 行权价加权 ✓)
- [ ] 费用:期权费/行权交割费入账(attribution.fees=1631 ✓)
- [ ] 双口径对账:premium 227,572 = 21 腿权利金×100 合计 ✓;realized 120,191 − terminal 78,733 = 41,458 = 3 腿未平仓权利金 ✓(attribution 视为已实现、terminal 计入浮亏,口径差异已解释)
- [ ] 行权率与 delta 自洽(94% 行权率 = 全 ITM,修复后应降)
- [ ] 未成交模型(unfilled 2/23 = 8.7%,机会成本 23,225 ✓)
- [ ] 期末权益自洽(realized 78,733 + unrealized −79,917 = −1,184 = 总权益变化 ✓)
- [ ] 修复后重跑:OTM 全覆盖、行权率 < 50%、正股已实现亏损 ≤ 手续费级

## Links

- 数据报告:reports/bt-HK.00700-123-7ecf5509.json(trajectory 75 步,21 腿明细见任务记录 dual-metric)
- 修复任务:#86(OTM/正股保护/covered_call_pct/双口径,codex 实施中)
- 重训任务:#87(00700 全历史,依赖 #86)
- 调研来源:
  - [ApexVol Wheel Strategy 完整指南与 5 年回测](https://apexvol.com/strategies/wheel-strategy#1)(delta 甜点表、50% 平仓、IV 过滤)
  - [ApexVol 回测页:50% max profit 跑赢持有到期](https://apexvol.com/strategies/wheel-strategy/backtest)
  - [ApexVol 期权策略回测对比(管理规则 > 策略选择)](https://apexvol.com/strategies/backtests)
  - [ApexVol wheel checklist(delta/DTE/股票选择)](https://apexvol.com/learn/wheel-strategy-checklist)
  - [VolRadar wheel 指南(covered call 0.2-0.3Δ、滚动)](https://volradar.com/learn/wheel-strategy-guide)
  - [Blue Collar Investor:covered call 行权价选择(OTM 进取 vs ITM 防御,never below breakeven)](https://www.thebluecollarinvestor.com/covered-call-strike-selection-when-using-the-pcp-or-wheel-strategy/)
  - [MarketXLS wheel 收入循环自动化(进一步 OTM 留空间)](https://marketxls.com/blog/ultimate-options-wheel-strategy-guide-for-traders)
  - [QuantWheel:7 大 wheel 错误(50% 目标价值 ~$3,600/年)](https://quantwheel.com/learn/wheel-strategy-biggest-mistakes)
  - [SPY/QQQ 实回测对比(nextlevelglobalacademy)](https://www.nextlevelglobalacademy.com/blog-posts/wheel-strategy-spy-qqq-options-income-2026)
