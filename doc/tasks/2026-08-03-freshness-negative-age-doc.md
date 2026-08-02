# 闭环 #89: DATA_PIPELINE 补 -max-age 负值拒绝文档(最新合入功能文档覆盖)

- **日期**: 2026-08-03
- **PR**: #295(功能)+ 本文档(归档)
- **背景**: 「最新合入功能文档覆盖」对账——#90(review P3)新增 `-max-age must not be negative` 守卫(exit 2),但 DATA_PIPELINE.md:77 阈值行未同步;全库 grep 确认 -max-age 仅 DATA_PIPELINE 提及,单点欠账。

## 改动

- doc/DATA_PIPELINE.md:77:`-max-age` 说明后补「负值拒绝(`-max-age must not be negative`,exit 2,2026-08-03)」

## 验证

- verify.sh 全绿;docs-only → CI 5/5(db-integration 29s)
- 行为核实:main.go:1391 `if *maxAge < 0` → return 2,与文档 exit 2 一致

## 备注

- **引擎经验(最新合入功能覆盖)**: 合入 review 修复(尤其 P3 细节)后,必须回到行为文档对账——#90 修了代码、文档漏同步,隔一轮才补上;「刚合入的行为」是最容易漏的文档欠账来源。
- **同轮另有**: UI 看盘类打磨被契约测试拦截(TestAppJSQuoteRemovedFromUI——老板 2026-08-02「不需要看盘工具」,UI 永不调 /v1/futu/quote;watchlist 现价列方向正确驳回),该约束已沉淀进 memory;14 个残留 worktree 已清理(本地运维,44M→0)。
- **候选池**: 仍枯竭(待老板 7 项)。
