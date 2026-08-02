# 闭环 #61: RELEASE_DAILY dev-up 冒烟项数同步

- **日期**: 2026-08-03
- **PR**: #240(功能)+ 本文档(归档)
- **背景**: AUTO_ADVANCE triage 文档对账——RELEASE_DAILY.md:29「跑 10 项验收 smoke」过时:dev-up.sh 实际 16 项(#43 扩展后未同步),与 ACCEPTANCE.md 术语(验收冒烟)也不一致。

## 改动

- `跑 10 项验收 smoke` → `跑 16 项验收冒烟`(grep -c 验证 dev-up.sh check 计数为 16)
- 核对 release.sh publish/republish/deploy 参数与文档一致,无欠账

## 验证

- docs-only → CI skip 路径 5/5

## 备注

- **引擎经验**: 文档里的数字(10 vs 16)是对账引擎新维度——「数字计数 vs 实际实现」,grep 脚本实际 check 数验证,不能信文档自述。
- **候选池**: 仍枯竭(待老板 7 项 + 微信小程序 blocked)。
