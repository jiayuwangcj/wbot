# 闭环 #62: ROADMAP/API 小程序放弃决策同步

- **日期**: 2026-08-03
- **PR**: #242(功能)+ 本文档(归档)
- **背景**: AUTO_ADVANCE triage 新对账维度「决策变更 vs ROADMAP 状态」——老板 2026-07-31 决策(discussions/21):放弃微信小程序(微信将下架含股票小程序),转 PC 端 Web(第一步)+ 移动 Web 框架预留;ROADMAP.md 最后更新 a06dbd2(v3 Futu 同步)未同步此决策,v3/v4 行仍写小程序为 Web UI 优先形态/前置依赖。

## 改动

- **ROADMAP.md v3 行**: 去「持仓数据 Web 化(微信小程序前置依赖…)」依赖链
- **ROADMAP.md v4 行**: Go API 去小程序前置依赖;Web UI 优先形态改「PC 端 Web(已实施:serve + 内嵌 UI)+ 移动 Web 框架预留」,注明决策出处(discussions/21)
- **API.md 3 处**: 数据面介绍「面向微信小程序/Web 前端」→「面向 Web 前端」;health 描述「微信小程序前置探测」→「serve 数据面前置探测」;footer 去小程序前置依赖

## 验证

- grep 全仓库:ROADMAP/API 小程序残留仅 v4 行「已放弃」决策记录本身(预期保留);docs-only → CI skip 路径 5/5

## 备注

- **引擎经验**: 新对账维度——「老板决策变更 × 跨文档同步」:决策记录在 discussions/21,但 ROADMAP(优先级形态)与 API.md(面向方/前置探测)各自独立演化,需 grep 决策关键词跨文档核对;历史归档 doc/tasks/*-miniapp-*.md 保留不动(记录当时状态)。
- **候选池**: 仍枯竭(待老板 7 项)。
