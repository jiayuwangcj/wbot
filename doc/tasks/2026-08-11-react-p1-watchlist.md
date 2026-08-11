# React 重构 P1:watchlist 页迁移

- **id**: `2026-08-11-react-p1-watchlist`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

把 `web/src/pages/watchlist/` 从占位页迁移为完整功能(React + TS strict,现代简洁不平铺)。

验收 = checklist `doc/tasks/2026-08-11-react-refactor-checklist.md` **Watchlist 21 项逐项等价**,含空态文案原样;布局现代:信号审计主表 + Wheel 编辑抽屉化(Drawer ≥640px 自适应,锚点行可用)。

## Constraints

- 只动 `web/src/pages/watchlist/**` + 自己的 html 壳 + 页面级 vitest;共享层(src/{api,hooks,lib,components})**只读**,签名已冻结(切片 0)
- PRIVACY 红线:永不调 /v1/futu/quote;admin 不在此页
- TS strict 零 any(FRONTEND.md 白名单外);注释 ≤1 行;不引入新依赖
- 深链 `#signal-<id>` / `#config-<symbol>-v<N>` 保留(hashchange)
- 30s 轮询 + visibilitychange(useAutoRefresh);编辑保存后重置表单与旧版一致

## Links

- Driven-By: doc/tasks/2026-08-11-react-refactor.md(切片 P1);Sol 评估验收项 1-10
- Checklist: doc/tasks/2026-08-11-react-refactor-checklist.md(Watchlist 21 项)
- 规范: doc/FRONTEND.md;共享层契约: web/src/api/types.ts(端点与 doc/API.md 对齐)
- Branch: `feat/slice1-watchlist`(worktree `.claude/worktrees/slice1-watchlist`)
- 执行者: codex(gpt-5.6-luna max)

## State

- **status**: `coding_done`(2026-08-11 提交 ea13e94,未 push)
- **last step**: 21 项等价实现(信号审计主表 + Wheel Drawer 编辑 15 条校验 + 观察列表 CRUD + 深链 #signal-* / #config-*-vN + 30s 轮询);build 通过;vitest 9 files/19 tests 全绿;go webui ok;dev-up 25/25;verify.sh 通过;无 futu/quote

## Next

- reviewer 评审(合入顺序 watchlist → data → results → admin,本片最先)
