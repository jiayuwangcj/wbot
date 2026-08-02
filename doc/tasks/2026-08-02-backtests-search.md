# 回测列表服务端全库搜索(q 参数) (S-backtests-search) — 2026-08-02

状态: ✅ 已合并 (PR #156, commit beb014e)

## 背景
AUTO_ADVANCE 根任务循环 ③ PLAN 优先级,候选评估后选中:回测数量
持续增长,本地 50 条内过滤(先 filter 后 sort)是价值上限。本次完成
「过滤 × 排序 × 分页」服务端化最后一环——搜索。

## 改动
1. **internal/backtest/store.go**: `ListResults` 追加 `q` 参数(在
   strategy 后、limit 前),SQL 加 `(symbol ILIKE $n ESCAPE '\' OR
   strategy ILIKE $n ESCAPE '\')`;`escapeLike` 转义 `%`/`_`/`\` 使
   q 按字面包含匹配。`LoadResults` 调用点传 `""` 保持行为。
2. **internal/httpapi/backtests.go**: `BacktestStore.List` interface、
   `backtestStore.List`、list handler 全部透传 `q`
   (`strings.TrimSpace(q.Get("q"))`)。
3. **internal/webui/web/app.js**(initResultsPage):
   - 搜索输入防抖 250ms:非空 → `applyFilter()`(本地即时反馈)+
     `loadSorted()`(服务端全库,权威);清空 → `loadSorted()` 恢复
     最近列表(避免残留搜索结果)。
   - `loadSorted` 拼接 `sort/order/q` 参数数组;回调仍走
     `applyFilter()`(服务端结果集上本地过滤幂等)。
4. 测试:
   - store 集成 `TestListResultsQueryIntegration`:symbol/strategy
     包含、大小写、专属前缀(防共享 dev 库历史记录污染 count)、
     字面 `%` 不吞全表、与精确过滤+limit 组合。
   - httpapi 契约:fake store `gotQ` + URL 编码 passthrough 断言;
     集成层 q 端到端(命中 1 条 + 无匹配 0 条)。
   - webui 契约断言更新(防抖/q 拼接/清空恢复/排序保留)。
   - 顺带修复 `TestBacktestExportIntegration` 时区敏感:断言写死
     UTC "Z" 格式,本机 TZ=Asia/Shanghai 时失败(集成测试开启后
     暴露),`t.Setenv("TZ", "UTC")` 固定。

## 验收
- `go test ./... -count=1` 全绿(19 包,含 WBOT_PG_DSN 集成)
- `gofmt -l` clean
- dev-up.sh smoke 10/10(--force 重启加载新二进制)
- 逐端点 15/15:UI 契约 5 + 真实 HTTP 8(±符号匹配/大小写/无匹配/
  字面 %/q+sort 全库重排/q+symbol 组合/空 q 默认)+ store 集成 +
  httpapi 集成
- CI: 5/5 全 pass 首轮绿;PR #156 merged

## 备注
- **运维坑**: dev-up.sh 在 serve 已运行时默认不重启(already_up==1
  且无 --force),服务端改动后验收必须 `scripts/dev-up.sh --force`。
- **验收脚本坑**: 环境无 jq → 用 node 内联 JSON 语义断言;DSN 探测
  容器 IP(`~/.wbot/dev` 是目录非 env 文件)。
- 前端交互语义:输入即 250ms 防抖全库搜索(权威),本地 filter 仅
  打字间隙的即时反馈;清空输入恢复最近列表,与「跨页排序」一致
  (排序参数与 q 同 URL 组合)。
