# Data 页覆盖表排序 (S-UI-coverage-sort) — 2026-08-02

状态: ✅ 已合并 (PR #135, commit fde7877)

## 背景
AUTO_ADVANCE 根任务循环 ⑥ 老板长期目标(UI 打磨):排序主题收官后
(#133 备注),队列回富途/IB 高频交互打磨——Data 页「已缓存数据」覆盖表
是找数据范围的高频入口(点击行 drill-in),此前首屏顺序取决于 broker
返回序,无排序能力。makeTableSorter 第四接入。

## 改动
1. **data.html**: coverage-table 表头加 `data-sort`
   (symbol/timeframe/count/min_ts/max_ts)+ `title="点击按此列排序"`;
   复权/年龄/新鲜度列保持不可排(freshness 是元素列,不参与比较)。
2. **app.js**: `COVERAGE_SORT_KEYS`——count `?? -Infinity` 沉底;
   min_ts/max_ts 为定长字符串字典序=时间序,无需解析。数据源是
   `/v1/admin/cluster` 快照(非分页),排序本地完成:`coverageRows`
   缓存最近数据,`sorter.render = () => renderCoverageRows(coverageRows)`
   本地重绘,无需服务端重查(与订单表同型,与回测跨页相反)。
   默认 `max_ts` 降序——新数据在前,找缓存范围首屏即达。
3. 测试: TestCoverageSortingJS 契约(data-sort 键 + 取值器 + 默认排序)。

## 验收
- `go test ./... -count=1` 全绿(19 包);`gofmt -l` clean
- dev-up.sh 10/10 smoke
- 逐端点契约 16/16:serve 实际吐出的 data.html/app.js 契约 +
  `/v1/admin/cluster` bars_coverage 数据源 5 排序键字段验证
- CI: 5/5 全 pass 首轮绿

## 备注
- makeTableSorter 接入面至此 4 表(回测/持仓/订单/覆盖),加跨页 API
  排序 + 2 表默认排序,UI 表格排序能力全覆盖。
- 数据源结构:bars_coverage 行字段
  (adjust/count/fresh/max_ts/max_ts_age_seconds/min_ts/symbol/timeframe),
  fresh 布尔在 freshnessCell 渲染。
- 候选后续:watchlist 页观察列表表排序(列少,价值低,暂缓);admin
  页 freshness 覆盖表排序(运营低频,暂缓);富途实盘下单 Web 化
  (待老板拍板,FUTU.md 交易安全策略)。
