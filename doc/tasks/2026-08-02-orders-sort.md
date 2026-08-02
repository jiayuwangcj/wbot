# 订单表表头排序 (S-UI-orders-sort) — 2026-08-02

状态: ✅ 已合并 (PR #129, commit a67934e)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨)连续推进:makeTableSorter
通用排序工厂(PR #123)已接入回测表(#121 前身)与持仓表,本轮第三个接入
点——订单表。三类表排序能力至此齐备。

## 改动
1. **index.html**: `orders-table` 表头加 `data-sort`
   (create_time/symbol/side/status/qty/price/fill_qty)+
   `title="点击按此列排序"`。
2. **app.js**: `ORDERS_SORT_KEYS` 取值器——数值列(qty/price/fill_qty)
   `?? -Infinity` 沉底语义,与跨页排序 API 的 NULLS LAST 一致;字符串列
   (create_time/symbol/side/status)直接取值走 localeCompare。
   接入点:`ordersSorter = makeTableSorter("orders-table", ORDERS_SORT_KEYS)`;
   `ordersSorter.render = loadOrders`(点击表头后重新拉取最新挂单);
   `renderOrders` 渲染前 `ordersSorter.sortItems(snap.orders)` 排序。
3. **测试**: `TestOrdersSortingJS` 契约断言(index.html data-sort 键、
   ORDERS_SORT_KEYS 内容、ordersSorter 接入点)。

## 验收
- `go test ./... -count=1` 全绿;`gofmt -l` clean(CI 门禁,先查后提交)
- dev-up.sh 10/10 smoke(serve + PG + futu 网关)
- 逐端点契约 14/14:serve 实际吐出的 index.html/app.js 契约 +
  `/v1/futu/orders?env=sim` 数据源字段验证(11 字段含全部排序键)
- CI: test / db-integration / governance / ci-summary 全 pass(首次轮询即绿)

## 备注
- 订单表为本地排序(数据源快照拉全量),与回测表跨页服务端排序不同——
  挂单量级小,本地排序足够;`render = loadOrders` 语义是「排序后刷新
  数据」而非「重查排序参数」。
- 方向/状态列为字符串枚举(buy/sell、submitted 等),排序按字典序,
  未做中英文映射排序。
- 候选后续:持仓表默认按市值降序初始排序(positions-sort 备注)。
