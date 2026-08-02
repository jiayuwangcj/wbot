# 闭环 #37: 资产曲线 touch 读数(移动端曲线惯例)

- **日期**: 2026-08-03
- **PR**: #194(功能+归档合一)
- **背景**: #36 的曲线 hover 读数仅桌面可用——移动端无 hover 概念。券商 App 移动端曲线交互惯例是「触摸即显读数」。老板 Goal ⑥ 延续。

## 改动(2 文件)

- `internal/webui/web/app.js`: `attachCurveHover` 重构——抽取 `showTip(clientX)`/`hideTip()` 共用核心;新增 `touchstart`/`touchmove`(即触即显,`preventDefault` + `passive: false` 让读数优先于页面滚动)与 `touchend`(隐藏);mouse 监听改 addEventListener 统一风格
- `internal/webui/webui_test.go`: 契约断言更新(mousemove/touchstart/touchend 的 addEventListener 形式)

无 HTML/CSS 改动(tooltip 元素与样式复用 #36)。

## 验证

- 19 包测试全过、gofmt 干净
- dev-up --force smoke: embed app.js 含 touch 监听(touchstart/touchend grep 命中)
- CI 5/5 全绿

## 备注

- **交互取舍**: 触摸即显(非长按)——与移动券商曲线一致且实现简单;`preventDefault` 使曲线区域触摸不滚动页面,120px 高的曲线区影响可接受。
- **候选池**: 仍枯竭;「UI 交互打磨」引擎两条候选(资产曲线 hover、touch)已全部落地。下一步候选: 无自主可推进项;等待老板拍板/资源/新需求。
