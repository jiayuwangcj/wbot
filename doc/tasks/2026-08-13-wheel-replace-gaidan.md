# 2026-08-13 wheel 改单(replace)支持:允许策略调整未成交挂单

## Goal

固化(wheel)策略此前对同一未成交合约反复 ALERT(US.JD260821P29000 挂单
206158430256,信号 747/749/750/751/752 全被 LLM 审核拒绝,重复敞口暴露)。
老板指令(2026-08-13):①候选选择排除「同合约+同方向已有未成交挂单」的合约;
②**允许策略调整已存在的未成交订单**(撤旧挂新,改单);③**改单同样需要
LLM 审核**——与全新订单同闸门,不绕过。

## Constraints

- 改单 = 撤 pending_orders 中旧挂单(replace.order_id/replace.contract)改挂首选候选,写操作,须过 LLM 审核(规则 7,硬性项)。
- 确认执行时**先撤旧单再下新单**;撤单失败 = 拒绝新单(旧单仍在,再下单即重复敞口),留痕人工处理。
- 更优合约上的挂单(被排除的 natural top)绝不换成次优候选,避免策略自毁式空转改单:仅当 rest 单已不在报价/结构校验失效、或新候选严格按选择排序优于 rest 单时触发。
- 改单只对「唯一同方向挂单」生效(多张同向挂单 → 不自动改,留给人工)。

## Links

- 前置修复:8d9180a(挂单排除,已合入未部署)
- LLM_REVIEW_FAILED 白名单双处修复:2fee07c + 16ac054 + migration 012(同链,已部署)
- 相关任务:doc/tasks/2026-08-13-llm-review-failure-handling.md

## State

- [x] wheel.Evaluate:挂单排除 + 唯一同向挂单时携带 Replace(纯函数)
- [x] 改单闸门:betterCandidate 排序比较(与候选选择同一排序,单一事实源)
- [x] migration 013:wheel_signals.replace jsonb
- [x] wheelstore:SignalRecord.Replace 读写(INSERT/scan/3 SELECT)
- [x] wheelrun:mapPending 带 OrderID、mapSignal 带 replace、审核规则 7 注入 + WHEEL_STRATEGY.md 同步(SSOT 测试)
- [x] telegram/discord 确认执行:cancel-before-place,撤单失败拒绝
- [x] 测试:wheel 6 场景(改单/同合约/多挂单/陈旧/失效/方向不匹配)+ TG/Discord 撤旧挂新 + 撤单失败拒绝
- [x] verify.sh 全绿
- [ ] 部署 + migration 013 落库确认 + 生产观察(改单卡片/确认流)

## Next

1. 部署(compose up -d --build),确认 013 迁移应用、schema 含 replace 列。
2. 观察 US.JD 推送链:排除生效(同合约不再重复 ALERT)→ 若出现改单信号,核对 LLM 审核输入含 replace/pending_orders。
3. 收口更新本记录。
