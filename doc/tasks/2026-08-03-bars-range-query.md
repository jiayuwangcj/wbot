# 排期:数据页 bars 查询时间范围输入

## 状态

**✅ 已完成**(2026-08-03)。

## 来源

AUTO_ADVANCE Round 37。「后端能力 vs API/前端暴露面」对账第二命中:`/v1/bars` 早已支持 from/to/limit/desc(API.md 全文档化),数据页表单只暴露尾部窗口(`limit=100&desc=1`)—「看某段区间 K 线」做不到。

## 变更

- `data.html` bars-form 加可选 `<input type="date" id="bars-from/bars-to">`(券商面板惯例:富途/IB K 线页都有起止日期);section-tag 加 `id="bars-range-tag"`(`最近 100 根` ↔ `指定区间`)
- `app.js`:新 `barsRangeFromInputs()`(date → RFC3339 闭区间:from 取 `T00:00:00Z`、to 取 `T23:59:59Z`——bars ts 为 UTC 收盘时刻,日线 bar 落在当天 UTC 内,边界直觉一致);loadBars 加 from/to:填范围走 `&from=..&to=..&limit=1000&desc=1`(范围内最近 1000 根、新在前),留空保持最近 100 根
- 范围输入只影响表单提交;行 drill-in 与补数据后刷新保持尾部窗口
- `webui_test.go` TestDataPageContract:HTML 输入断言 + JS 断言(`barsRangeFromInputs`、`&limit=1000&desc=1`、`"指定区间"`、URL 拼装新形式 `"/v1/bars?" + q + range`)

## 验证

- verify.sh 连跑两遍全绿
- E2E 真实 PG:`/v1/bars?symbol=HK.00700&timeframe=1d&from=2026-01-01T00:00:00Z&to=2026-08-03T23:59:59Z&limit=1000&desc=1` 返回 2026 年内日线(真实落库数据);data.html 渲染确认两个 date 输入 + tag
- CI 五检查全绿;#319 merge --admin

## 收益

K 线查看从「只能最近 100 根」到「任意区间」;与 CLI/API 参数面一致(同一 RFC3339 语义)。**引擎经验:①E2E 前必须重建二进制——旧二进制会给出误导性「改动没生效」结果(Round 35 遗留进程同源);②「API 文档已写、前端没暴露」也是欠账——对账维度从代码扩展到文档 vs 前端调用**。

## 后续

老板决策(2026-08-03):回测/运行/策略三入口拆分——数据页将随回测入口重组,本页 range 输入语义保留。
