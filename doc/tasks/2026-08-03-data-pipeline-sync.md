# 闭环 #58: DATA_PIPELINE 命令表同步

- **日期**: 2026-08-03
- **PR**: #234(功能)+ 本文档(归档)
- **背景**: AUTO_ADVANCE triage 文档对账——DATA_PIPELINE「命令一览」表(9-15 行)与 CLI 实际 dispatch 逐条比对,发现三处欠账。

## 改动

- **命令一览表补 `wbot ingest account` 行**: #29 已实现、§账户资产快照独立小节(59 行)也在,但入口表漏行——「命令一览」是新成员入口,看表会漏掉账户快照命令(顺带交叉引用 [[FUTU]] §9)
- **freshness 表行补期权区块**: 表行原本只写 symbol×timeframe,§数据新鲜度小节(73 行)已有期权说明,表行同步
- **任务轨迹更新**: 「至 2026-07-31-ingest-time-range.md 止」→ 收录 2026-08-02/08-03 六个 ingest 闭环(mock-rangeflags / refill / options-ingest-button / futu-ingest-account-doc / ci-option-freshness)

## 验证

- docs-only → CI 走 skip 路径,5/5 全绿(db-integration 21s 为 skip 报告)

## 备注

- **引擎经验**: 「命令一览」这类入口表与实现逐行对账是低成本高价值动作——实现有新命令(account 于 #29)时入口表不更新,新成员/文档消费者会漏命令;独立小节存在 ≠ 入口表同步。
- **候选池**: 仍枯竭(待老板 7 项 + 微信小程序 blocked)。
