# 行情明细面板打磨——周期 tab 切换 + bars 涨跌幅列 (S-detail-tabs) — 2026-08-03

状态: ✅ 已合并 (PR #177)

## 背景
AUTO_ADVANCE 根任务循环。候选池枯竭(用户三候选已完整落地、待老板
7 项不可自主推进)后,按老板 Goal ②(无需求时参照富途/IB/Schwab
打磨 UI)取最小步——富途 K 线视图两处标准交互 wbot 还缺:明细面板
周期一键切换(此前需回顶部表单改下拉)、bars 表无涨跌幅列。

## 改动
1. **data.html**:detail-body 顶部加 `detail-tabs` 周期 tab 栏
   (role=tablist);bars 表加「涨跌幅」列头。
2. **style.css**:`.tabs` 样式(主题变量 --border/--muted/--accent,
   active 高亮,非 active hover 描边,深浅色自适应)。
3. **app.js**:
   - `DETAIL_TIMEFRAMES`(1m/5m/15m/30m/60m/1d/1w/1mo,与顶部表单
     一致)+ 模块级 `detailSymbol/detailAdjust`(loadBars 记忆,
     覆盖表行点击进入后 tab 切换沿用当前标的/复权)。
   - `renderDetailTabs(timeframe)`:生成 tab、当前高亮、点击 →
     loadBars(symbol, tf, adjust)。
   - `renderBarsDetail`:加涨跌幅列——相对前一根(desc 序数组后
     一项,即更早一根)收盘,`up/down` 语义色,最旧一根「—」。
4. **webui_test.go**:TestDataPageContract 补断言(detail-tabs /
   renderDetailTabs / DETAIL_TIMEFRAMES / 涨跌幅)。

## 验证
- `go test ./... -count=1` 全绿(19 包);`gofmt -l .` 干净
- dev-up smoke 10/10(新二进制自动重启)
- 真实端点:data.html/app.js/style.css embed 内容验证(grep
  detail-tabs/涨跌幅/renderDetailTabs/.tabs)+ bars API
  HK.00700 1d fwd 真实数据正常(7/29-7/31)
- CI 5/5 全绿首轮过

## 备注
- **tab 与表单一致性**:tab 8 档与顶部 timeframe 下拉同源;切 tab
  会同步回填顶部表单值(loadBars 既有行为),两种入口状态一致。
- **涨跌幅语义**:wbot 的 up/down 语义色在主题中已定义(浅色
  红涨绿跌、深色适配),涨跌幅列直接复用,无新色定义。
- 后续候选(待拍板/资源):资产曲线(账户历史快照,大)、期权链
  ATM/ITM/OTM 标记(富途期权链交互,中)。
