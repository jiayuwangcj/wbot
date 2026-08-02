# /v1/futu/orders 订单列表只读端点(proto 代理)

- **id**: `2026-08-02-futu-orders-endpoint`
- **created**: `2026-08-02`
- **updated**: `2026-08-02`

## Done

- PR #97 已合并:`GET /v1/futu/orders?env=&acc_id=&pending=`(默认 `pending=1` 挂单;字段白名单 order_id/symbol/name/side/status/qty/price/fill_qty/fill_avg_price/create_time/update_time/last_err)。
- 共享 `protoClient`(futu_client.go):单 TradeClient + 互斥串行 + isConnError 掉线重连;`futuAccounter` 重构到其上,futuOrderer 同路径 — 两端点继承 PR #96 的自愈。
- `TradeClient.Orders(ctx, acc, pendingOnly)`:`GetOrderListForAccount` + `PendingOrderStatuses()`。
- fakegw `OrdersBody` + proto round-trip/参数测试;serve 注册 + help + API.md。
- 验收(本地全链,老板规则):`go test -race`/`vet` 全绿;serve + 真实网关重启 → `/v1/futu/orders` 返回真实挂单 `HK.00700 Buy 100 @ 1 Submitted`;坏参数 400;account/quote/options/strategies/watchlist/backtests/admin 全 200;CI 五检全 pass。

## Goal

Dashboard 订单状态面板(老板 2026-08-02 指令:Data 页改 Dashboard,右栏显示当前订单状态)需要账户订单列表;浏览器无法直连网关,serve 代代理 proto 接口。切片 A(后端)先行,前端切片 B 依赖它。

## Constraints

- 只读:不撤单不改状态;默认 sim(安全红线),real 只读查询(与 CLI 同一策略)。
- 白名单响应,不泄漏账户/订单元数据(PRIVACY)。
- 复用 account 的 503/502 错误映射与连接管理(不复制重连逻辑)。

## Links

- 需求源:老板 2026-08-02 Dashboard 指令;切片计划 `doc/issues/draft-2026-08-02-dashboard.md`
- 现状:`internal/httpapi/futu_client.go`、`futu_orders.go`、`futu_account.go`(重构)、`internal/futu/trade.go`、`internal/futu/fakegw/fakegw.go`
- 前置:PR #95(FUTU_PROTO_ADDR 双地址)、#96(重连)

## State

- **status**: `done`
