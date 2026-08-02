# 闭环 #55: ACCEPTANCE.md 与 CI 远程门同步

- **日期**: 2026-08-03
- **PR**: #228(功能)+ 本文档(归档)
- **背景**: #52/#53 落地后,doc/ACCEPTANCE.md(#51)矩阵未反映: 4 个脚本已入 CI 自动跑、检查数已变化(backtest 19→21、watchlist 14→16)。

## 改动

- 脚本表新增 CI 列: accept-paper/accept-agent-federation ✅ test job;accept-backtest/accept-watchlist ✅ db-integration
- 检查数同步(总计 122→126);覆盖矩阵补 from_watchlist/buy-hold 说明

## 验证

- CI 5/5(docs-only)

## 备注

- **引擎经验**: 验收体系自身的文档(#51 总表)也会过期——「验收脚本 × CI 门」是新的对账维度,每个改动验收体系的闭环应顺带刷新总表。
- **候选池**: 仍枯竭。
