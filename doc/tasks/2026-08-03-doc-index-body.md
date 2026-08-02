# 闭环 #88: doc 索引漏链 pinned_discussion_body(文档索引对账延伸)

- **日期**: 2026-08-03
- **PR**: #293(功能)+ 本文档(归档)
- **背景**: 文档索引完整性对账——doc/README.md 条目清单列了 24/25 个 doc/*.md,`pinned_discussion_body.md`(发帖正文模板,被 GITHUB_DISCUSSION_OPS.md:48/58 引用)未列。与 #85 索引对账同族:条目清单必须覆盖实际文件集。

## 改动

- doc/README.md:20:`[[pinned_discussion]]` 行补链 `[[pinned_discussion_body]]` 并说明引用方式(经 [[GITHUB_DISCUSSION_OPS]])

## 验证

- 全语料复扫:doc/ 下 25 个 md 全部可索引(README 自排除);GITHUB_DISCUSSION_OPS.md:73 的裸名链 [[pinned_discussion_body]] 原本可解析,现索引直达
- verify.sh 全绿;docs-only → CI 5/5

## 备注

- **引擎经验(文档索引对账延伸)**: 索引完整性对账 = 「索引条目集 vs 目录实际文件集」差集检查;#85 查链是否悬空,#88 查链是否存在(漏列)——两类互补,漏列更隐蔽因为导航不出错只是找不到。
- **候选池**: 仍枯竭(待老板 7 项)。
