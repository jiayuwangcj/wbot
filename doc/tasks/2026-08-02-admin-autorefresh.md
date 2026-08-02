# Admin 页 30s 自动轮询 (S-UI-admin-autorefresh) — 2026-08-02

状态: ✅ 已合并 (PR #137, commit ea9aaec)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨):interaction-feedback
(#115)备注候选「自动轮询(Dashboard/Admin 定时刷新)未做——futu 网关
调用成本与页面可见性控制需单独设计」。Dashboard 当时已有轮询,Dashboard
数据源含 futu 网关(account/orders)故成本敏感;Admin 的 cluster/config
均为 PG 本地查询,无网关成本——本轮补齐 Admin 侧,并顺手参数化
startAutoRefresh 消除「每页重复 visibilitychange 样板」。

## 改动
1. **startAutoRefresh(fn) 参数化**: 按页注入刷新函数,`autoRefreshFn`
   模块级记忆——visibilitychange 恢复时 `startAutoRefresh()` 无参调用
   仍沿用本页函数(不再硬编码 loadDashboard)。可见时才拉取、后台停
   (既有成本控制语义不变)。
2. **initAdminPage**: `startAutoRefresh(loadAll)` + visibilitychange
   监听——cluster/config 30s 轮询,运维盯盘免手点刷新;手动刷新按钮
   共存。
3. 测试: TestAdminAutoRefreshJS 新契约;TestAutoRefreshJS/
   TestInteractionFeedbackJS 断言同步新形态(旧断言
   `"visible") loadDashboard();` 已改 `(autoRefreshFn || loadDashboard)()`)。

## 验收
- `go test ./... -count=1` 全绿(19 包);`gofmt -l` clean
- dev-up.sh 10/10 smoke
- 逐端点契约 9/9:serve 实际吐出的 app.js 参数化轮询契约 +
  cluster/config 200 + version 字段稳定性验证
- CI: 5/5 全 pass 首轮绿

## 备注
- 验收小坑 1:cluster 整包 md5 对比误报——uptime_seconds 秒级变化本就
  该不同,改为对比稳定字段 components.process.version。
- 验收小坑 2:cluster 顶层仅 `components`(无顶层 version),version 在
  components.process.version。
- Data 页覆盖表同源 cluster,但刷新按钮 + 排序已够用,未加自动轮询
  (低频操作页,保持克制)。
