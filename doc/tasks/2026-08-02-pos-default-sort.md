# 持仓表默认按市值降序 (S-UI-pos-default-sort) — 2026-08-02

状态: ✅ 已合并 (PR #131, commit 36b5279)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨):makeTableSorter 三表
接入完成后,补排序体验缺口——持仓表初始无排序(空白 ↕),首屏顺序
取决于 broker 返回序。按券商面板惯例(富途/IB 均市值降序在前)设默认。

## 改动
1. **makeTableSorter** 暴露 `renderIndicators`:init 时设置默认排序键后
   需要同步表头 ↑/↓ 指示(原为闭包私有,首屏显示 ↕ 与真实排序不符)。
2. **initDashboardPage**: 工厂创建后设 `state.key = "market_val"`、
   `state.dir = -1` + `renderIndicators()`,首屏即市值降序;
   `sortItems` 已处理空键回退,此默认在用户点击表头后正常接管。
3. 持仓数值列 getter(qty/avg_cost/price/market_val)对齐
   `?? -Infinity` 沉底——此前仅 pl 有,缺字段时 `undefined - number`
   产生 NaN 破坏比较;与订单表全部数值列一致。
4. 测试: TestPositionsSortingJS 契约补默认排序 + 沉底语义断言。

## 验收
- `go test ./... -count=1` 全绿(19 包);`gofmt -l` clean
- dev-up.sh 10/10 smoke
- 逐端点契约 6/6:serve 实际吐出的 app.js 默认排序契约 +
  `/v1/futu/account?env=sim` 数据源 market_val 字段验证
- CI: 5/5 全 pass 首轮绿

## 备注
- 数据源注意:持仓/订单在 `/v1/futu/account?env=sim` 快照内,无独立
  /v1/futu/positions 端点(契约脚本首版 404,已修正)。
- 沉底语义一致性:前端 `?? -Infinity` ↔ 服务端 `NULLS LAST`(sort-api)。
- 候选后续:订单表默认按时间降序(新单在上)同理一行;回测表已服务端
  默认 id DESC。
