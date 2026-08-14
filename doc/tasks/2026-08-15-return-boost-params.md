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
- [ ] 评审
- [ ] 00700 全历史重训(进 #87 训练范围;训练时空间含 min_iv_rank 自动扩快照查询 1 年窗口)

## 实施备注(2026-08-15 coder)

- 平仓触发价取 quote.Ask(保守),结算价取 st.OptPrice(最新 close=bid),结算利润 ≥ 触发阈值,方向一致
- 持仓合约越过 DTE 窗口仍可平仓(适配器从完整批次补回持仓报价;这些报价进候选循环照旧被 DTE 规则拒绝,报告契约不变)
- 实盘 wheelrun:AvgPremium 来自 futu GetCostPrice;live 无 IV rank 历史数据源,min_iv_rank>0 时 HOLD fail-closed(排期)
- delta_max 默认 0.30(schema/template 显式默认);domain 层 >0 启用、0 禁用;设 1.0 等效无限(逃逸通道)

## Links

- 调研:doc/tasks/2026-08-15-wheel-optimization-research.md(证据与来源)
- 修复:#86(OEM 前置条件)
- 重训:#87(本任务合入后执行)
