# 闭环 #64: doc/README 索引补两条

- **日期**: 2026-08-03
- **PR**: #246(功能)+ 本文档(归档)
- **背景**: AUTO_ADVANCE triage 对账维度「doc/README.md 索引 vs 顶层 doc 文件双向核对」——RELEASE_DAILY.md(07-31 创建,f861d3f)与 DATA_STANDARD.md(08-01 创建,d757fcc)均被 DATA_PIPELINE/API/ORGS/FUTU 引用,但索引(08-03 仍更新,997a217)缺这两条。

## 改动

- 索引补 `[[RELEASE_DAILY]]`(日构建与本地部署,插在 CI_REPORT 后)与 `[[DATA_STANDARD]]`(数据标准,插在 DATA_PIPELINE 后)两行
- 顺带验证索引全部 `[[X]]` 链接可解析(tasks/README 存在、proposals/0001 通过 doc/proposals/ 解析),无其他缺口

## 验证

- 双向核对脚本:24 个顶层 doc 文件 vs 索引引用,仅上述两条缺口,已补;docs-only → CI skip 路径 5/5

## 备注

- **引擎经验**: 新对账维度——「索引 vs 目录双向核对」:grep 索引全部 [[X]] 引用逐一解析 + ls 顶层文件逐一比索引,双向各查一遍才能同时抓「漏列」(文件有索引无)与「死链」(索引有文件无);对账要覆盖「双向」。
- **候选池**: 仍枯竭(待老板 7 项)。
