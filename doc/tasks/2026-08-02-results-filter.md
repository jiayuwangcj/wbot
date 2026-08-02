# 回测结果列表过滤 (S-UI-results-filter) — 2026-08-02

状态: ✅ 已合并 (PR #141, commit 7bf6a40)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨):非阻塞候选消化后
自主继续——找特定标的/策略的回测是高频操作(复跑、对比、汇报),列表
仅 50 条无过滤入口,只能翻页找。参考富途/IB 工作台列表交互补过滤。

## 改动
1. **results.html**: 列表区顶部 `type=search` 输入框
   (`#results-filter`,placeholder「按代码/策略过滤(如 00700 或
   covered-call)」,autocomplete off)。
2. **app.js**: `applyFilter` 先过滤后排序——filter(symbol/strategy
   小写包含匹配)→ `sortItems`;与跨页排序组合(排序后仍过滤);
   `loadSorted` 重载后同样过 filter;空态文案按过滤态区分
   「无匹配「q」的回测结果。」vs 默认「暂无回测结果…」
   (`EMPTY_DEFAULT` 缓存初始文本,清空恢复)。
3. 测试: TestResultsSortingJS 契约扩展(filter 元素 + applyFilter
   逻辑 + `sortItems(list)` 新形态——render 统一走 applyFilter)。

## 验收
- `go test ./... -count=1` 全绿(19 包);`gofmt -l` clean
- dev-up.sh 10/10 smoke
- 逐端点契约 9/9:serve 实际吐出的 results.html/app.js 契约 +
  `/v1/backtests` 列表 symbol/strategy 过滤键字段验证
- CI: 5/5 全 pass 首轮绿

## 备注
- 过滤是本地(当前 50 条),排序是服务端跨页——语义:过滤结果再按
  全局排序排列;跨页重载保留过滤词。全库过滤(服务端 q 参数)列
  后续候选(需 ListResults 加搜索参数,数据量大再上)。
- 未加防抖:50 条本地 filter 成本可忽略。
