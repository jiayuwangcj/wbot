# 订单表默认按时间降序 (S-UI-orders-default-sort) — 2026-08-02

状态: ✅ 已合并 (PR #133, commit a8a9737)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨):pos-default-sort
(#131)备注「订单表默认按时间降序同理一行」——本轮落地,两表首屏
排序缺口补齐。

## 改动
- **initDashboardPage**: `ordersSorter` 创建后设
  `state.key = "create_time"`、`state.dir = -1` + `renderIndicators()`,
  首屏新单在上(券商挂单面板惯例),表头同步 ↓ 指示。
- 测试: TestOrdersSortingJS 契约补默认排序断言。

## 验收
- `go test ./... -count=1` 全绿(19 包);`gofmt -l` clean
- dev-up.sh 10/10 smoke
- 逐端点契约 4/4:serve 实际吐出的 app.js 默认排序契约 +
  `/v1/futu/orders?env=sim` create_time 数据源(1 条挂单,格式
  `YYYY-MM-DD HH:MM:SS` 字符串,localeCompare 降序即可保证新单在上)
- CI: 5/5 全 pass 首轮绿

## 备注
- 排序主题至此收官:三表排序(#121/#123/#129)+ 跨页 API 排序(#127)
  + 两表默认排序(#131/#133)。create_time 为 `YYYY-MM-DD HH:MM:SS`
  定长字符串,字典序=时间序,无需转换。
- 后续候选回到:富途/IB 高频交互打磨(自选股/详情页/下单表单)、
  ingest -from/-to 时间范围、数据源 Provider 抽象、外部 cron 文档化。
