# 交互反馈补全 (S-UI-feedback) — 2026-08-02

状态: ✅ 已合并 (PR #115, commit b02af01)

## 背景
AUTO_ADVANCE 根任务循环 ⑤ UI 打磨连续推进:文案中文化(PR #113)之后,
扫描发现两处反馈缺失——watchlist 保存成功无显式确认(只有列表刷新)、
admin 集群页节点状态只在进页加载一次(无刷新入口)。

## 改动
1. **watchlist 保存成功反馈** (`watchlist.html` `watchlist-form-ok` +
   `app.js`): 提交成功后显式提示「已保存 symbol(strategy)」(ok 色);
   编辑行(beginEdit)/策略卡点击/切换策略/提交失败时隐藏(hideOk)。
2. **admin 页刷新按钮** (`admin.html` `admin-refresh` + `app.js`
   `loadAll`): 一键重载 cluster + config;初始加载也走 loadAll(单一
   路径)。
3. 删除确认弹窗文案中文化:「从观察列表移除 X?」。
4. 测试: `webui_test.go` TestInteractionFeedbackJS(JS 契约)+ 元素断言
   (watchlist-form-ok / admin-refresh)。

## 验收
- `go test ./... -count=1` 全绿
- dev-up.sh 10/10 smoke(serve + PG + futu 网关)
- 逐端点契约 6/6 + watchlist PUT 真实保存路径(200)+ cluster API
  数据源(`components` 结构)验证
- CI: test / db-integration / governance / ci-summary 全 pass

## 备注
- 验收小坑: `/v1/admin/cluster` 顶层是 `components` 字段,脚本首版断言
  `"name"` 误报失败(与回测详情/account snap 的顶层字段名不同),已修正。
- 自动轮询(Dashboard/Admin 定时刷新)未做——futu 网关调用成本与页面
  可见性控制需单独设计,列为后续候选。
