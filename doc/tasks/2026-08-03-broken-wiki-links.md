# 闭环 #85: 修复悬空 wiki 链接(0001 迁入 proposals/ 后 4 处链接未跟随)

- **日期**: 2026-08-03
- **PR**: #287(功能)+ 本文档(归档)
- **背景**: 文档索引对账——`doc/0001-automation-baseline.md` 迁入 `doc/proposals/` 后,4 处 `[[0001-automation-baseline]]` wiki 链接未跟随(README 索引 + PLAN_V0/WORKFLOW/ROADMAP 关联段),导航全部指向不存在路径。

## 改动

- 4 处 `[[0001-automation-baseline]]` → `[[proposals/0001-automation-baseline]]`
- 全语料复扫(根 README + doc/ 含子目录)后清零;排除两类非坏链:同文件锚点(`[[#错误]]`,API.md:668 有 `## 错误` 标题)、裸名约定可解析链(`[[2026-07-31-web-v1-target]]` → doc/tasks/ 下真实文件、`[[draft-2026-08-01-strategy-options]]` → doc/issues/ 下真实文件)

## 验证

- 全语料扫描 0 悬空;docs-only → CI 5/5(check-skip)

## 备注

- **引擎经验(文档索引对账)**: ①文件迁移(目录结构调整)是悬空链接高发点——迁完必须全库扫 `[[旧名]]`;②判定坏链要区分三类:真悬空(文件不存在)、同文件锚点(`#` 开头)、裸名约定链(Obsidian vault 按名解析,跨目录合法)——扫出后逐个验证,不能机械全改;③「同一误述整链存在须全库清零」经验再次应验(4 处同病)。
- **候选池**: 仍枯竭(待老板 7 项)。
