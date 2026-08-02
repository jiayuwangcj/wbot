# 回测结果表表头排序 (S-UI-results-sort) — 2026-08-02

状态: ✅ 已合并 (PR #121, commit 5315df8)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨)连续推进:枚举中文化
(PR #119)后扫描券商面板(富途/IB/嘉信)高频交互——表格排序是回测/持仓
列表的标准能力,wbot 所有表格均无排序。回测结果最多 50 条/页,按收益/
回撤/时间排序是核心诉求,取此最小切片。

## 改动
1. `results.html`:表头 th 加 `data-sort` 键(id/strategy/symbol/equity/
   total_return/max_drawdown/bars/created_at)+ `点击按此列排序` title。
2. `app.js`:
   - `RESULTS_SORT_KEYS` 取值器:数值列按值比较、字符串列按字典序,
     metric 缺失按 -Infinity 沉底(不置顶)。
   - `sortResults` 复制后排序(不改原数组,对比勾选不受影响)。
   - `renderSortIndicators`:↕(未排)/↑(升)/↓(降)表头指示,首次点击
     升序、同列再点切换。
   - `initResultsSorting`:表头点击绑定,重绘走统一 render 路径。
   - 排序重绘后恢复详情选中高亮(`openDetailId` + `selectResultsRow`,
     与既有 `tr.selected` 样式联动)。
3. 测试: `webui_test.go` TestResultsSortingJS(表头 data-sort 契约 +
   JS 逻辑契约)。

## 验收
- `go test ./... -count=1` 全绿
- dev-up.sh 10/10 smoke(serve + PG + futu 网关)
- 逐端点契约 21/21:results.html/app.js 契约 + backtests 数据源字段
  (metrics 含 equity/total_return/max_drawdown/bars)
- CI: test / db-integration / governance / ci-summary 全 pass

## 备注
- 排序只做「当前页列表」——列表本身是 /v1/backtests?limit=50 的最近
  50 条。跨页全局排序需 API 层 sort 参数,列为后续候选。
- 勾选对比在排序重绘后保留(compareSelection 独立于渲染),已确认。
- 主题化扫描顺带完成:style.css 全部颜色走 design token(90 处
  var(--),无硬编码色),深色模式自动适配,无需改动。
