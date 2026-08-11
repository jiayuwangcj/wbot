# React 重构 P4:admin 页迁移

- **id**: `2026-08-11-react-p4-admin`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

把 `web/src/pages/admin/` 从占位页迁移为完整功能(React + TS strict,现代简洁不平铺)。

验收 = checklist `doc/tasks/2026-08-11-react-refactor-checklist.md` **Admin 9 项逐项等价**;布局现代:集群 4 卡(Card + 状态徽标),配置/Telegram 向导 Tabs,向导 Steps 化。

## Constraints

- 只动 `web/src/pages/admin/**` + 自己的 html 壳 + 页面级 vitest;共享层**只读**
- **PRIVACY 红线(本片重点)**:admin 配置**只写不读**(配置值绝不回显);红线 grep dist 无配置值;零外链(BotFather 外链豁免);评审要求:红线测试落实(Go 侧 dist grep + 页面行为)
- TS strict 零 any;注释 ≤1 行;不引入新依赖
- Telegram 向导:密码框、BotFather token 外链豁免(https://t.me/BotFather)

## Links

- Driven-By: doc/tasks/2026-08-11-react-refactor.md(切片 P4);Sol 评估:配置只写不读与 Telegram 向导 Steps 化
- Checklist: doc/tasks/2026-08-11-react-refactor-checklist.md(Admin 9 项)
- 规范: doc/FRONTEND.md;共享层契约: web/src/api/types.ts
- Branch: `feat/slice4-admin`(worktree `.claude/worktrees/slice4-admin`)
- 执行者: Claude coder subagent

## State

- **status**: `reviewed_pending_merge`(2026-08-11)
- **last step**: reviewer B **通过**(feature,无 P0/P1/P2);9/9 等价达标,安全红线三重落实(类型层无 value 字段 + 无读值渲染路径 + 行为断言不回显);672daf4 待合入

## Next

- 合入 main(顺序 watchlist → data → results → admin,本片压轴);P3 观察排期:保存成功但刷新失败时「已保存。」语义(可并入一致性打磨)
