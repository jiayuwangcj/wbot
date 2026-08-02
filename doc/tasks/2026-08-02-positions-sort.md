# 持仓表表头排序 + makeTableSorter 通用化 (S-UI-positions-sort) — 2026-08-02

状态: ✅ 已合并 (PR #123, commit 2a135f8)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨)连续推进:回测结果表
排序(PR #121)落地后,持仓表是券商面板(富途/IB)排序价值最高的表之一
——盯盘时按市值/浮盈排列是标准交互。顺带把排序逻辑从回测表专属
重构为通用工厂,消除重复。

## 改动
1. **通用工厂** `makeTableSorter(tableId, getters)`(app.js):表头
   data-sort 键 → 取值器;点击切换升/降序, ↕/↑/↓ 指示;数字列按值、
   字符串列按字典序,缺失值按 -Infinity 沉底;`sorter.render` 由调用方
   注入(点击后重绘)。
2. **回测结果表迁移**:RESULTS_SORT_KEYS 保留为取值器,排序状态/指示器/
   绑定逻辑全部走工厂(resultsSorter),PR #121 的独立实现删除。
3. **持仓表接入**:POSITIONS_SORT_KEYS(symbol/qty/avg_cost/price/
   market_val/pl,盈亏缺失沉底);renderPositions 渲染前 sortItems;
   env 切换(sim/real)时排序状态保留(sorter 模块级)。
4. index.html 持仓表头加 data-sort + `点击按此列排序` title。
5. 测试: TestResultsSortingJS 同步新 API;TestPositionsSortingJS 新增。

## 验收
- `go test ./... -count=1` 全绿
- dev-up.sh 10/10 smoke(serve + PG + futu 网关)
- 逐端点契约 16/16:index.html 表头契约 + app.js 工厂/双表接入 +
  sim 账户 positions 字段(avg_cost/market_val/pl/price/qty/symbol)
- CI: test / db-integration / governance / ci-summary 全 pass

## 备注
- 通用工厂已接两张表;观察列表/订单表如需排序,加 data-sort 键 +
  取值器即可,无需新逻辑。
- 默认不排序(保持 API 返回顺序);如需富途惯例「持仓默认按市值排」,
  可在 initDashboardPage 初始化时设初始排序,列为后续候选。
