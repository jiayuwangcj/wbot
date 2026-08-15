# 美股模拟盘下单失败排查(2026-08-15 老板:「刚刚又提示了一笔,但是仍然下单失败,检查是否实装了」→ 老板补充:「908号单」)

## Goal

排查 JD 美股期权模拟盘人工确认下单「失败」原因,确认实装是否生效,并回答「怎么调通」。

## 结论(2026-08-15 盘中实验,证据闭合)

**实装已生效;908 失败是 #60 校验正确拦截;根因在券商侧:富途 OpenD 对美股模拟盘的 PlaceOrder 只返回 stub,不产生真实订单,美股模拟盘 API 下单通道不可用。**

## 证据链

### 1. 908 号单全链路(2026-08-14 16:56 UTC)

- LLM_REVIEW=APPROVE(16:59:45)→ 推送 → 人工确认 → `place OK acc=13477966 US.JD260821P29000 sell 1 @ 0.53 -> order_id=206158430256 order_id_ex="0"` → #60 校验 `orderIDEx=="0"` → REJECTED「order unconfirmed」。
- 实装确认:serve 日志含「order unconfirmed」→ 镜像已含 #60 校验(commit 5c58192,8/14 00:35 部署)。

### 2. 盘中实验(2026-08-15 美东 13:14-13:30,交易时段)

**实验 A:期权 P29000 sell 1 @ 0.30**(老板:「查一下,一定把 JD 的模拟盘调通」后续验证):

| 检查 | 结果 |
| --- | --- |
| PlaceOrder 返回 | order_id=206158430256 / order_id_ex="0"(与历史 5 次 CONFIRM 744/757/761/765 同号) |
| 下单后 8 分钟持仓 | 无 P29000(与基线相同:仅 P28500 -1/SPCX 1000/INTC 1000) |
| 资金 | cash 672741.349 分文未动(无权利金结算),frozen=0 |
| 订单列表(pending=0 全量) | 空 |
| 撤单尝试 | 「订单不在本地缓存. 请先调 /api/orders 刷新, 或在请求里传 orderIDEx (= backend szOrderID)」— 网关自己都没有这张单 |
| 排除延迟撮合 | 8 分钟 ≫ 网络传闻的 5 分 tick 周期 |

→ **订单从未进入券商后端,从未真实生效**。与昨天收盘后测试(3.0 单标记 FILLED 但无结算)同机制。

**实验 B:JD 正股 buy 100 @ 29.00**(老板指令:「下单一笔 JD 正股看看流程是否正确」):
- PlaceOrder 返回同一 stub(order_id=206158430256 / order_id_ex="0")
- 撤单报「Can't edit filled orders」(网关把 stub 标记 FILLED,但无成交结算)
- 5 分钟后:持仓无 JD、cash 672741.349 分文未动、frozen=0、订单列表空
- → **美股正股同样不生效,问题覆盖美股市场全部(正股+期权)**

### 2b. 网关日志(futu-opend-rs 容器)逐环节证据

- `place_order::stub: stub order upsert 到 cache (broker/user confirm pending 决定 stub 是否对 client 可见) order_id_ex=0 need_op_confirm=true is_pending_broker_confirm=true`
- `post_ack: real-trade fill callback guard: pending stub TTL reached without broker push → refreshed orders before purge`(TTL 到期无券商 push → 清除)
- 撤单:期权「订单不在本地缓存」(stub 已 purge);正股「Can't edit filled orders」(backend_code=-1)
- **港股对照(13477968):need_op_confirm=true / is_pending_broker_confirm=true 与美股完全相同,唯一差异 = order_id_ex=7571417 等真实后端单号** → 确认流程正常走通
- → **wbot 与网关链路完全正常,富途 OpenAPI 对美股模拟盘下单不返回后端订单号、不撮合、不结算(stub 30 秒 TTL 后 purge)**

### 3. 对照组

- **港股模拟盘同网关同链路**:CONFIRM 返回真实券商单号(7571417/7572257/7572265/7572283/7572292/7575771),持仓逐一对应生效(TCH260821P430000 -1=P687、C450000 -2=P692、P450000 -1=P500)。→ 网关链路本身正常。
- **老板 app 手动下单**(P28500 -1,成本 0.35):真实生效 → 美股模拟盘账户本身支持美股期权交易,只是 **OpenAPI 通道**不下真单。

## 判定

- #60 校验**正确**(orderIDEx="0" = 订单未真实生效,拦截防「假确认」),不改。
- 之前误判「#60 误伤美股模拟盘」的修复方向(放宽 sim 校验)**已废弃**:放宽只会推送假「已下单」。
- 富途官方对模拟盘 API 下单与 app 数据互通的描述在美股期权模拟盘上不成立(实测 stub 不入簿)。

### 4. 官方方法验证(老板指令:「用官方方法尝试一下」,官方 futu-api SDK 10.10.7008)

对照官方文档 https://openapi.futunn.com/futu-api-doc/qa/trade.html 逐项核对并实测:

| 官方要求 | 实测 |
| --- | --- |
| 美股模拟交易须 STOCK_AND_OPTION 账户 | **✓ 13477966 = STOCK_AND_OPTION**(sim_acc_type=4) |
| 限价单 + DAY + trd_env=SIMULATE | ✓(OrderType.NORMAL / DAY / SIMULATE) |
| 美股盘中支持(盘前盘后仅融资融券模拟账户) | ✓ 美东 13:18 盘中 |
| 价格档位(美股股票 $1 以上 $0.01) | ✓ 29.00 |
| 模拟盘无需解锁 | ✓(自动跳过) |
| 免责协议错误(Q11 各券商链接) | 未出现(ret=0,无 err_msg) |
| **官方 SDK place_order(US.JD buy 1 @ 29.00)** | **ret=0 但 order_id=0、order_status=SUBMITTING → 网关同样 stub(pending broker confirm)→ 无真实订单** |

- 官方 SDK 连接自建网关成功(协议兼容),账号枚举/下单全部 ret=0(无任何错误码),但**富途后端不返回券商订单号(order_id_ex=0)** → 单不生效
- 三次实验(Go 期权/Go 正股/官方 SDK 正股)行为完全一致 → **排除 wbot 客户端、自建网关、参数、账号类型,问题在富途 OpenAPI 美股模拟盘下单通道本身(接受请求但不产生真实订单)**
- 官方文档无模拟撮合机制说明(网络检索无公开技术细节),「官方 OpenD 本地撮合」无证据

## 待办(调通路径)

- [ ] 查 futu-opend-rs 网关版本/富途 OpenAPI 美股期权模拟盘交易支持状态(官方文档:美股模拟交易账号类型要求 STOCK_AND_OPTION;我们的 13477966 枚举为 AllInOne——可能 OpenD/SDK 版本差异,需核对)
- [ ] 若 OpenD 版本过旧:升级 OpenD + SDK 后重试盘中实验
- [ ] 富途客服/社区确认:美股期权模拟盘是否支持 OpenAPI 下单(有已知不支持的说法)
- [ ] 过渡方案(若券商侧短期无解):美股测试期由人工 app 下单(wheel 推送 + LLM 审核照旧,确认后人工在 app 执行),wbot 只记录 REJECTED+原因,不推假确认

## State

- [x] 实装确认(#60 已部署,serve 日志可见)
- [x] 908 失败原因定位(#60 正确拦截 stub)
- [x] 盘中实验(美股模拟盘 OpenAPI 下单不生效,三重证据)
- [x] 港股对照(网关链路正常,港股全部生效)
- [ ] 券商侧支持核对(OpenD 版本/官方文档/客服)
- [ ] 调通或过渡方案落地

## Links

- #60:5c58192「fix(order): 真实下单留痕 + stub 订单号校验 + watchFill 未确认警示」
- 前置:#85 doc/tasks/2026-08-15-jd-sim-order.md
- 富途官方:openapi.futunn.com/futu-api-doc(美股模拟交易 STOCK_AND_OPTION 账号类型说明)
