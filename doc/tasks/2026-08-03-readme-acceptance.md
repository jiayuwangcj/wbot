# 闭环 #60: 根 README 本地开发节补验收体系入口

- **日期**: 2026-08-03
- **PR**: #238(功能)+ 本文档(归档)
- **背景**: AUTO_ADVANCE triage「README 导航」维度对账——根 README 是 GitHub 仓库首页第一屏,本地开发节只有 `go test`/`go vet`,真实提交门(verify.sh ≡ CI test job、dev-up 16 项冒烟、12 个 accept 脚本 126 项)零提及;doc/README.md 有 ACCEPTANCE 链接(#51),根 README 没有。

## 改动

- 本地开发节补 `scripts/verify.sh`(提交前全量校验,≡ CI test job)与 `scripts/dev-up.sh`(本地全链冒烟 16 项)两行
- 补「逐端点验收:12 个 accept-*.sh 126 项,零依赖对与 PG 依赖对已在 CI 自动跑,索引 doc/ACCEPTANCE.md」指引

## 验证

- docs-only → CI skip 路径 5/5

## 备注

- **引擎经验**: 入口文档对账要覆盖「README 导航」的全部层级——doc/README.md 有索引 ≠ 根 README 有新成员指引;仓库首页第一屏(本地开发节)是验收体系的第一可见入口,与 ACCEPTANCE.md(#51)、doc/README 导航同属「运维沉淀」层。
- **候选池**: 仍枯竭(待老板 7 项 + 微信小程序 blocked)。
