# wheel 收益提升参数:50% 利润平仓 + delta 目标区间 + IV rank 过滤(2026-08-15 老板目标:「搜搜方案提高收益率完善 wheel 策略」)

## Goal

按调研结论(doc/tasks/2026-08-15-wheel-optimization-research.md)实施三大收益率提升项,全部进 ES 搜索空间,供 #87 00700 全历史重训使用。依赖:#86 修复合入(OTM/正股保护/covered_call_pct/双口径)。

## 改动点

1. **50% 利润平仓 `profit_take_pct`(第一优先,行业证据:4-5× net P&L)**
   - 回测引擎当前只有持有到期+行权两种退出(close_cost 恒 0),需支持中途平仓:已收权利金回落到 max_profit×profit_take_pct 时买回平仓(释放保证金+资金周转)
   - 参数:profit_take_pct ∈ [0, 0.8](0 = 持有到期,默认 0 保持兼容);进 ES 搜索空间(如 [0.3, 0.7])
   - 实盘 wheelrun 同步:持仓期权按该规则输出平仓建议(ALERT 语义,人工确认闭环不变)
   - 数据面:平仓腿计入 option_close_cost(报告 attribution 已有该字段,当前恒 0)
2. **delta 目标区间 `put_delta_max` / `call_delta_max`(行业甜点 25-30Δ)**
   - 候选过滤/排序:卖出期权 delta ≤ 上限(put_delta_max 默认 0.30, call_delta_max 默认 0.30);OTM 硬约束之上再限 delta,双保险(深 OTM 低权利金无意义,25-30Δ 是收益/行权率平衡点)
   - 进 ES 搜索空间(如 [0.15, 0.40])
   - 与 covered_call_pct 关系:covered_call_pct 控行权价下限(≥成本),delta 控上行偏移,二者独立可联合寻优
3. **IV rank 过滤 `min_iv_rank`(行业:IV Rank > 35 才卖)**
   - 数据面已有官方 IV(HKEX RP006),按标的 1 年历史 IV 百分位算 rank
   - min_iv_rank ∈ [0, 1],低于阈值不卖(候选全 mask,HOLD);默认 0 兼容
   - 进 ES 搜索空间(如 [0, 0.5])
   - 实盘:低 IV 时段跳过卖出(或拉宽 strike/延长 DTE,先做跳过,其余排期)

## 约束

- worktree:复用 #86 worktree 合入后新分支(或主会话指定);verify.sh 全绿;署名按实际编写模型
- 兼容:所有新参数默认值 = 现行为(profit_take 0 / delta_max 大值等价无限 / min_iv_rank 0),存量配置不破坏
- 单测:平仓逻辑(权利金回落触发/时间衰减)、delta 过滤、IV rank 过滤(高/低 IV 两场景)
- 真实 PG 回测:00700 数据跑一次,确认平仓触发、delta 过滤生效

## State

- [x] 派单(依赖 #86 合入已满足;2026-08-15 codex 额度尽,退回 Claude 侧 coder 执行,codex 额度 08-20 恢复)
- [x] 实施 + verify.sh 全绿(2026-08-15 分支 feat/return-boost-params;单测:平仓边界/多仓选优/长腿跳过/delta 过滤/IV rank 高-低-未知/e2e 平仓归因 OptionCloseCost=100、IV rank 21 批序列、ES 空间范围校验;PG 集成测因 WBOT_PG_DSN 未设置 skip)
- [x] 评审:有条件合入(feature),条件 1-3 已修复(2026-08-15 追加提交,verify.sh 全绿)
- [x] 第二轮复核修复(P1-A 链外腿接线与标的过滤/P1-B 平仓推送确认链路/P2 平仓冷却窗;2026-08-15 追加提交,verify.sh 全绿)
- [ ] 00700 全历史重训(进 #87 训练范围;训练时空间含 min_iv_rank 自动扩快照查询 1 年窗口)

## 实施备注(2026-08-15 coder)

- 平仓触发价取 quote.Ask(保守),结算价取 st.OptPrice(最新 close=bid),结算利润 ≥ 触发阈值,方向一致
- 持仓合约越过 DTE 窗口仍可平仓(适配器从完整批次补回持仓报价;这些报价进候选循环照旧被 DTE 规则拒绝,报告契约不变)
- 实盘 wheelrun:AvgPremium 来自 futu GetCostPrice;live 无 IV rank 历史数据源,min_iv_rank>0 时 HOLD fail-closed(排期)
- delta_max 默认 0(schema/template 显式默认 0 = 现行为,与 profit_take_pct/min_iv_rank 一致,2026-08-15 评审条件 1);domain 层 >0 启用、0 禁用;设 1.0 等效无限(逃逸通道)
- 评审条件 2:LLM 审核闸门补平仓语义(wheelReviewRules/llmreview 第 8 条平仓审核,rules_ssot_test 断言,doc/API.md 与 doc/WHEEL_STRATEGY.md 同步)
- 评审条件 3:实盘链外持仓经 ClosePositions 纳入平仓评估(域层排除出库存口径,库存计算不变);close_position ALERT 豁免 suppressRepeatAlert 30 分钟抑制窗(P3)
- 评审 P2:ivrank 严格小于百分位(恒定 IV 序列 rank 恒 0 不穿门槛),补平台段/恒定序列测试
- 第二轮修复(2026-08-15,复核报告 2026-08-15-return-boost-params-r2.md):
  - P1-A(a):runner 补拉循环把链外腿报价放进 quotes map 仅用于 Delta/LotSize 填充,从未并入 in.Quotes → 实盘链外平仓永不触发(positionQuote 找不到);修复:closeQuotes 并入 in.Quotes(Expiry 从代码解析)
  - P1-A(b):closePositionLegs 按链底层字母过滤(00700→TCH),其他标的空腿不评估、不进 LLM 审核输入;顺带解决跨标的腿每 pass 限频拉报价浪费
  - P1-B:平仓载荷独立落库 close_position/close_qty/close_quote(migration 015);Telegram+Discord 走独立 BUY 渲染/确认路径(side=buy、数量=空腿张数、限价=ask 退 last);卖向 firstCandidate/#57 严格化一字未动
  - P2:平仓独立冷却窗 closeAlertCooldown=90min(closeAt 基线,窗口不滚动),卖向 30min 窗不变;e2e:fake futu 衰减报价 → 平仓 ALERT 落库 → LLM APPROVE → 卡片 buy 语义 → yes 确认 buy 下单

## Links

- 调研:doc/tasks/2026-08-15-wheel-optimization-research.md(证据与来源)
- 修复:#86(OEM 前置条件)
- 重训:#87(本任务合入后执行)
