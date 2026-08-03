# 排期:资产曲线 hover 读数圆点描边主题化(#313)

## 状态

**✅ 已完成**(2026-08-03)。

## 来源

AUTO_ADVANCE 候选池巡检:三候选(ingest -from/-to、Provider 抽象、外部 cron 文档化)再次实证全已实现;候选池声称枯竭,但按老板 Goal「主题化 + 打磨 UI 不要停」深挖主题化维度——app.js 全 canvas 着色对账(`strokeStyle`/`fillStyle` 逐一核对)发现**唯一漏网硬编码**:equity/asset 曲线 hover 读数高亮圆点 `strokeStyle = "#fff"`。

## 证据链

- `internal/webui/webui_test.go:287` 注释「hardcoded #fff replaced by tokens」——CSS 侧 `#fff` 早已 token 化,但 **canvas 绘制路径漏检**(此前对账只查 style.css,未查 app.js 绘制代码)
- `app.js` 其余 canvas 着色全走 `CURVE_*` 调色板 helper(`cssVar("--accent") || fallback` 模式,1375-1378)+ 828 行涨跌色同样模式;仅 1788 行硬编码
- 后果:深色主题下 hover 圆点白色描边突兀(与 `--surface` #161b22 不匹配),主题切换不跟随

## 修复

- `app.js:1788`:`ctx.strokeStyle = cssVar("--surface") || "#fff"`——浅色主题行为不变(默认值即白色),深色主题取 `--surface`;与 `CURVE_*` 模式一致,主题切换随 redrawCharts 正确刷新
- `webui_test.go`:TestThemeSystem 追加断言 `cssVar("--surface") || "#fff"`,防未来 canvas 硬编码回归

## 验证

- go test ./internal/webui/ 绿;verify.sh 连跑两遍全绿;CI 五检查全绿
- 对账:app.js 全部 strokeStyle/fillStyle 现均走主题变量,零硬编码

## 收益

主题化收口:canvas 绘制路径与 CSS 同源;防回归断言就位。**引擎经验:主题化对账必须双路径——style.css 变量 + app.js canvas 绘制各查一遍;「CSS 已 token 化」的既有注释是线索,提示同主题下 JS 侧可能有漏网**。
