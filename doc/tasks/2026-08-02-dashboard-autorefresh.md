# Dashboard 自动轮询 (S-UI-autorefresh) — 2026-08-02

状态: ✅ 已合并 (PR #117, commit 28e2870)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨)连续推进:交互反馈
(PR #115)落盘文档中备注的候选——Dashboard 定时刷新。富途/IB 面板的
实时性是核心交互,此前只在进页/手动刷新/env 切换时加载。

## 改动
1. `startAutoRefresh`/`stopAutoRefresh` (`app.js`): setInterval 30s 调
   `loadDashboard`(sim+real 账户快照 + 订单)。
2. **页面隐藏暂停**: visibilitychange → hidden 时 clearInterval,visible
   时恢复——避免后台标签页持续打 futu 网关。
3. tick 内二次检查 `visibilityState === "visible"`(双保险,即使 timer
   未被清理也不在后台刷新)。
4. 手动刷新按钮与自动轮询共存(loadDashboard 幂等)。
5. 测试: `webui_test.go` TestAutoRefreshJS(契约:常量/两函数/可见性判断/
   visibilitychange 接线)。

## 验收
- `go test ./... -count=1` 全绿
- dev-up.sh 10/10 smoke(serve + PG + futu 网关)
- 逐端点契约 5/5 + sim/real account 数据源验证(`funds` 字段,轮询路径
  每 30s 各打一次)
- CI: test / db-integration / governance / ci-summary 全 pass

## 备注
- 验收小坑: account API 顶层是 `env`/`acc_id`/`funds`/`positions`(无
  `snap` 包装),且 `head -c 40` 会截断在 `"funds` 之前——两个断言错误
  均为脚本问题,非服务问题。
- 轮询间隔 30s 为保守值;futu 网关为本地容器(REST 成本低),后续如需
  更实时可下调(结合 tick 可见性双保险)。
