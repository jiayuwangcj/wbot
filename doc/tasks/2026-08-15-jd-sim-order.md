# JD 美股期权模拟盘调通验证(2026-08-15 老板指令:「一定把 JD 的模拟盘调通,不然美股无法测试」)

## Goal

验证 JD 美股期权在富途模拟盘全链路可用(账号/资金/持仓/行情/下单/撤单),确保美股 wheel 测试可行。

## 账号解析(futu accounts)

| ACC_ID | ENV | SIM_TYPE | 市场 | 用途 |
| --- | --- | --- | --- | --- |
| 1907141 | sim | stock(不支持期权) | HK(1) | 港股正股 |
| 1907143 | sim | stock | CN(3) | 大陆正股 |
| 13477968 | sim | option(仅期权) | HK(1) | 港股期权 |
| **13477966** | sim | **all-in-one** | **US(2)** | **美股正股+期权 ← JD 用这个** |

## 验证结果(2026-08-15 早,美东周五收盘后)

- [x] **账号/资金**:13477966 总资产 91.3 万,现金 67.3 万,购买力 158.5 万,frozen=0 ✓
- [x] **持仓**:JD260821P28500 **-1 手(卖 put,成本 0.35,现价 0.28,浮盈 $7)** — 策略此前已真实成交,持仓可见 ✓;另有 SPCX 1000 / INTC 1000
- [x] **行情**:JD 期权 bid/ask/vol/OI 全量正常(signal 889/890 的候选报价来自实盘快照)✓
- [x] **下单通道**:`futu order -symbol US.JD260821P29000 -side sell -qty 1 -price 3.0 -acc-id 13477966` → 网关接受,返回 order_id=206158430256 ✓
- [x] **撤单通道**:`futu cancel` 能提交,网关返回订单真实状态 ✓
- [x] **查询 refresh_cache**:Positions/Orders 均传 refresh_cache=true(trade.go:366/385)✓

## ⚠ 发现:模拟盘收盘后撮合特性(与实盘核心差异)

- 测试单挂 **3.0 卖价**(远高于 ask 0.5),撤单报 `Can't edit filled orders` → 网关标记**已成交(FILLED)**
- 但该「成交」**无持仓、无资金结算**:positions 无 P29000;frozen_cash=0;total_assets 无变化;订单查询(pending=0 全量)为空
- 结论:美股模拟撮合引擎**收盘后不按价格严格撮合**,挂单被直接标记 FILLED(与网上反馈一致:「模拟撮合依赖模拟盘自己的订单簿/撮合时点,与实盘不一致」);结算按交易日处理(周一开盘后复查)
- **教训:模拟盘测试下单必须在美股交易时段(美东 9:30–16:00)进行,价格按合理限价(bid 卖/ask 买);收盘后的单会被假成交且不可撤**

## 遗留

- [ ] 周一(8/17)美股开盘后:复查持仓是否出现 P29000 -1(若出现 → 买回平掉,保持账户干净);同时在交易时段做一笔真实限价单撮合验证(挂 bid 价的卖单应正常成交)
- [ ] watchFill/confirmOrder 与模拟盘「假成交/订单查询为空」的交互:若订单列表查不到已结束订单,watchFill 可能持续 pending 警示(需观察)
- [ ] 真实下单仍走人工处置闭环(推送 → LLM 审核 → 人工确认下单),本验证只证明通道可用

## Links

- 前置:doc/tasks/2026-08-14-param-install-00700-jd.md(#84,JD 参数实装)
- 账号路由:internal/futu/trade.go(AccountForSymbol / #55 美股期权下单账户路由)
- CLI:cmd/wbot/futu.go(order/cancel/funds/position/accounts)
- 网络调研:模拟盘撮合机制/5 分 tick/末日平仓难成交/STOCK_AND_OPTION 账号要求(2026-08-15 会话)
