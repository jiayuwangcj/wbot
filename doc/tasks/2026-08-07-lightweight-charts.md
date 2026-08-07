# 排期:数据页 K 线引入成熟库(TradingView Lightweight Charts)

## 状态

**✅ 已完成**(2026-08-07)。PR #321。

## 来源

老板决策(2026-08-03):「K线前端找成熟框架或者UI前端控件来做,可自行寻找tradeview,futu等页面,用成熟库不要自己开发」。选型 TradingView Lightweight Charts v4.2.3(Apache 2.0,standalone 单文件)。

## 变更

- `internal/webui/web/vendor/lightweight-charts.standalone.production.js`(163,684B,v4.2.3,许可头含 Apache 2.0;go:embed 自动包含,零构建链、离线可用)
- `data.html`:明细区 `canvas#detail-sparkline` → `div#detail-chart`(蜡烛图容器);OHLC 表格保留(十字线 + 表格并存);vendor 脚本先于 app.js 加载
- `app.js`:`renderCandlestickChart`(bars 为 `&desc=1` 新在前,喂图前反转为时间升序;`createChart` 实例只建一次,`setData` 换数据保留缩放/平移;库未加载静默降级,表格兜底);`applyChartTheme` 主题切换走 `applyOptions`,颜色全跟 CSS token(涨 `--ok` / 跌 `--down`,与全站折线惯例一致);`redrawCharts` 扩展调用
- `style.css`:`#detail-chart` 容器(360px 高、边框圆角、背景随 `--surface`)
- `webui_test.go`:TestDataPageContract 更新(`detail-chart` + vendor script + JS 断言 `CandlestickSeries`/`createChart`/`applyChartTheme`/`detailSeries.setData(data)`/`typeof LightweightCharts`);新增 TestVendorLightweightCharts(vendor 存在 + ≥100KB + Apache 许可头);TestNoExternalURLs 豁免 `web/vendor/`(许可头 URL 非运行时外链)
- 资产/权益/对比折线**保持自研**(指令范围是 K 线;TestAppJSEquityCurveDrawing 禁 d3/chart.js/echarts 断言保留)

## 验证

- verify.sh 连跑两遍全绿(含 staticcheck)
- E2E 真实 PG:`/ui/data.html` 200、`/ui/vendor/lightweight-charts.standalone.production.js` 200(163,684B)、`/v1/bars` HK.00700 日线正常返回(ts 为 `+08:00` 偏移 RFC3339,`Date.parse` 兼容)
- CI 五检查全绿;#321 merge --admin

## 收益

K 线渲染从自研 canvas 折线升级为成熟库蜡烛图(缩放/平移/十字线开箱即用);vendored 单文件保持离线可用 + 版本固定;Apache 2.0 商用合规。

## 后续

老板新指令(2026-08-07,见 `2026-08-07-daily-data-completeness.md`):每日数据齐全检查独立模块——K 线完整性的系统化保障,下一闭环排期。
