# 闭环 #81: 长表限高 + sticky 表头(UI 打磨)

- **日期**: 2026-08-03
- **PR**: #279(功能)+ 本文档(归档)
- **背景**: 老板目标「无新需求时参考富途/IB/嘉信打磨 UI」——Data 页 K 线表补数据后数百行,页面滚动时表头不跟随,列名迷失;富途/IB 桌面端惯例为表头固定。

## 改动

- internal/webui/web/style.css:`.table-scroll` 加 `max-height: 65vh` + `overflow-y: auto`(短表不受影响);`th` 加 `position: sticky; top: 0; z-index: 1` + `background: var(--surface)`(不透明背景遮挡滚动内容,与 section 卡片背景一致,深浅主题自动适配)

## 验证

- verify.sh 全绿(19 包 + vet + race + staticcheck + CLI smoke);dev-up 19 项全过;serve 实测 `/ui/style.css` 含 sticky 规则;PR #279 CI 全绿

## 备注

- **UI 打磨经验**: sticky 表头的关键不是 sticky 本身,而是**容器限高**(overflow 容器内 sticky 才生效)与**不透明背景**(否则滚动内容穿透表头)——背景取表格所在卡片的 --surface,深浅主题天然适配。
- **候选池**: 仍枯竭(待老板 7 项)。
