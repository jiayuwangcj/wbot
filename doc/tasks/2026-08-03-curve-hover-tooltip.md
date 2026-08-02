# 闭环 #36: 资产曲线 hover 读数(富途/IB 曲线惯例)

- **日期**: 2026-08-03
- **PR**: #192(功能+归档合一)
- **背景**: 资产曲线四段(#179 数据层 / #181 UI / #183 调度文档 / #186 API 契约)闭合后,曲线本身仍是**静态 sparkline**——悬停无反馈。对照富途/IB 账户曲线交互(悬停读数),补交易软件标配的 hover 读数。老板 Goal ⑥(参照券商交互优点打磨 UI)的直接落地。

## 改动(4 文件)

- `internal/webui/web/app.js`: 新增 `attachCurveHover(canvas, pts)`——`canvas.onmousemove` 按 x 比例映射最近数据点,浮动标签显示 `fmtTime(captured_at) + " · " + fmtAccountMoney(total_assets)`(时间+总资产);`onmouseleave` 隐藏;左/底边界防溢出;`renderSummaryCurve` 画线后调用
- `internal/webui/web/index.html`: `#summary-curve-wrap` 内新增 `<div id="summary-curve-tip" class="curve-tooltip" hidden>`
- `internal/webui/web/style.css`: `#summary-curve-wrap { position: relative }` + `.curve-tooltip`(主题变量 bg/border/fg/shadow、`pointer-events: none` 防闪烁、nowrap、z-index 10)
- `internal/webui/webui_test.go`: 契约断言 +3(index.html tip 元素;app.js attachCurveHover/onmousemove/读数格式)

## 边界(刻意不做)

- `chartCache` 主题重绘不涉及(只绑定事件,不重绘)
- 空序列分支不绑定 hover(无数据无读数)
- canvas 键盘无障碍(tabindex/aria)保留;touch 悬停为后续候选(移动端无 hover 概念,需长按/点击方案,另议)
- `drawSparkline` 未动(共享函数,watchlist/明细页复用)——hover 只挂资产曲线

## 验证

- 19 包测试全过、gofmt 干净
- dev-up --force 重启 smoke:embed 内容 grep 含 `summary-curve-tip`(index.html)与 `attachCurveHover`(app.js)
- CI 5/5 全绿

## 备注

- **候选池**: 仍枯竭;本轮引擎换回**「UI 交互打磨」**(老板 Goal ⑥)——对账富途/IB 交互惯例 vs 当前 UI,资产曲线无读数是最明显差距。后续同引擎候选: 资产曲线 touch 长按读数(移动端)、bars 明细图 hover、回测 equity 曲线 hover(结果页 canvas 有 readout 已实现——`#curve-readout`,1723/1807 行已有 hover 读数,说明回测曲线已做过,资产曲线是遗漏)。
- 下一步候选: 无自主可推进项;等待老板拍板/资源/新需求。
