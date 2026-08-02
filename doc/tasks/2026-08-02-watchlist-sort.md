# watchlist 表排序(全站一致性) (S-watchlist-sort) — 2026-08-02

状态: ✅ 已合并 (PR #166, commit 350992b)

## 背景
AUTO_ADVANCE 根任务循环。审计全站 5 张表格:positions / orders /
coverage / results 均有 `makeTableSorter` 表头排序,唯 watchlist
(老板日常使用页)无排序——不一致且与券商面板惯例相悖。

## 改动
1. **watchlist.html** 表头 `data-sort`:symbol / strategy /
   updated_at(参数列跳过:JSON 字符串排序无意义)。
2. **app.js**:
   - `WATCHLIST_SORT_KEYS` 键集(与 RESULTS/POSITIONS/ORDERS/
     COVERAGE 同款模式)。
   - `watchlistSorter = makeTableSorter("watchlist-table", ...)`;
     render 闭包引用模块级 `watchlistItems` 缓存(行内编辑/回测/
     删除回调不变)。
   - `loadWatchlist` 结果经 `watchlistSorter.sortItems(items)`
     再渲染;默认 `updated_at` 降序(新更新在上,全站「新数据在
     前」惯例)。
3. 契约测试:`TestWatchlistSortJS`(键集/绑定/sortItems/默认排序/
   renderIndicators)+ TestWatchlistPageElements 表头断言 +
   TestWatchlistBacktestJS 旧断言更新为 sortItems 包装形式。

## 验证
- `go test ./... -count=1` 全绿(19 包,含 PG 集成)
- dev-up smoke 10/10(二进制变化自动重启)
- 逐端点验收 11/11:表头契约 ×4(含「参数列无 data-sort」)、
  app.js 接线 ×5、行按钮回调不回归 ×2
- CI: 5/5 全 pass 首轮绿;PR #166 merged

## 备注
- **为什么参数列不排序**:JSON 字符串字典序对 `{"param":...}` 无
  用户意义;5 张表中仅此一列无 data-sort,与 orders 表「价格/数量」
  按值排序的取舍一致。
- **TDZ 注意点**:`watchlistSorter` 为 init 闭包内 const,声明于
  `loadWatchlist`(function 声明,hoisted)之后,但首次调用在 init
  末尾(962 行),晚于 const 初始化,无 TDZ 问题。
- **render 闭包为什么缓存 items**:makeTableSorter 的 render 无参,
  需要能访问「最近一次列表」;模块级 `watchlistItems` 由
  loadWatchlist 每次写入,排序切换时直接用缓存重渲(与
  positionsSorter.render = renderPositions 读全局 snapByEnv 同理)。
