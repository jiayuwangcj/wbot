# 闭环 #39: 刷新按钮忙态(wrapRefreshClick)

- **日期**: 2026-08-03
- **PR**: #198(功能+测试合一,无独立归档 PR —— 归档文档与功能同轮提交)
- **背景**: 「UI 交互打磨」引擎延续。券商面板惯例: 操作进行中必须可见。三个页面(Dashboard/Admin/Data)的刷新按钮点击后无任何反馈——加载期间用户不知道是否已触发,重复点击会并发请求;参考富途/IB 刷新控件的忙态约定(禁用 + 文案变化)补齐。

## 改动

`internal/webui/web/app.js`:

- 新增 `wrapRefreshClick(id, busyText, fn)` helper: 点击时禁用按钮 + 文案换 busyText,`Promise.resolve(fn()).finally(...)` 恢复——自动轮询刷新不触发按钮态
- 三处刷新绑定改造:
  - `dash-refresh` → `wrapRefreshClick("dash-refresh", "刷新中…", loadDashboard)`
  - `admin-refresh` → `wrapRefreshClick("admin-refresh", "刷新中…", loadAll)`;`loadAll` 改为 `() => Promise.all([...]).then(() => stampUpdated("admin-updated"))` 返回 Promise(原为裸调用,不返回 Promise 则 busy 态立即恢复)
  - `data-refresh` → `wrapRefreshClick("data-refresh", "刷新中…", () => { ...; return loadDataCoverage().catch(...) })`

`internal/webui/webui_test.go`: TestInteractionFeedbackJS / TestAdminAutoRefreshJS 断言从旧 `addEventListener("click", loadAll)` 形式更新为 `wrapRefreshClick("admin-refresh"` 形式,`loadAll` 断言同步为 `() => Promise.all([` 新形状。

## 验证

- 19 包测试全绿 + gofmt 干净
- dev-up --force smoke 10/10
- embed 内容 grep: wrapRefreshClick/刷新中 5 处命中(helper 定义 + 三处绑定 + busyText)
- CI 5/5(governance/test/db-integration/check-skip/ci-summary)

## 备注

- **实现细节**: `Promise.resolve(fn()).finally()` 是关键——fn 可以是 Promise(loadAll/loadDataCoverage.catch)也可以是同步函数,finally 统一恢复;自动轮询(startAutoRefresh 内部调用 fn)不经按钮点击路径,不会误触发按钮态。
- **测试教训**: 改 JS 实现时 webui_test 的契约断言是逐字串匹配,重构后旧断言串必挂——本轮正是靠测试抓住 `loadAll` 形状变化(裸 `() => {` → `() => Promise.all([`)。
- **候选池**: 仍枯竭;「UI 交互打磨」引擎候选列表(hover 读数/touch 读数/忙态)落地完毕;下一步以探索引擎找最小步或等待老板拍板/资源/新需求。
