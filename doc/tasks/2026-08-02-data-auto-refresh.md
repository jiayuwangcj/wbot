# 数据页自动轮询(与 Admin 一致) (S-data-auto-refresh) — 2026-08-02

状态: ✅ 已合并 (PR #162, commit 62a76c7)

## 背景
AUTO_ADVANCE 根任务循环。候选池评估时发现:Admin 页有 30s 自动
轮询(`startAutoRefresh`, #137 参数化),Data 页只有手动刷新按钮;
coverage 表展示数据新鲜度,「补数据」落地后其价值上升——补完
自动浮现,无需手动点刷新。

## 改动
`internal/webui/web/app.js` `initDataPage`:
1. `startAutoRefresh(() => loadDataCoverage().catch(() => {}))` —
   复用 #137 参数化基础设施,与 Admin `startAutoRefresh(loadAll)`
   同款 30s 节奏;interval 只在页面可见时拉取(隐藏即停)。
2. 轮询路径静默吞错:瞬时失败下一 tick 重试,不刷屏;首载与
   手动刷新路径(`.catch(showError)`)保持显错不变。
3. visibilitychange 停/启监听,与 Dashboard/Admin 页模式一致。
4. `TestDataAutoRefreshJS` webui 契约:接线字符串 + 错误路径 +
   visibilitychange 模式。

## 验证
- `go test ./... -count=1` 全绿(19 包,含 PG 集成)
- dev-up smoke 10/10
- 逐端点验收 8/8:data.html 契约 ×2(按钮/错误槽)、serve 分发
  app.js 接线契约 ×4(注入/吞错/停/启)、轮询目标
  `/v1/admin/cluster` 200 + bars_coverage 有行
- CI: 5/5 全 pass 首轮绿;PR #162 merged

## 备注
- **为什么轮询路径吞错而首载不吞**:后台刷新失败是瞬时的,下一
  tick 自动重试;显式错误提示属于用户手动操作的语义。
- **验收脚本坑**:环境 `FORCE_COLOR=3`,node `console.log` 输出带
  ANSI 颜色码(`\033[33m1\033[39m`)→ 断言不可用字符串比较
  `console.log` 输出;用既有惯例 `node -e "...if(!(expr))
  process.exit(1)"` 退出码断言。
