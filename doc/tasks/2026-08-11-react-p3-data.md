# React 重构 P3:data 页迁移

- **id**: `2026-08-11-react-p3-data`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

把 `web/src/pages/data/` 从占位页迁移为完整功能(React + TS strict,现代简洁不平铺)。

验收 = checklist `doc/tasks/2026-08-11-react-refactor-checklist.md` **Data 14 项逐项等价**,含空态文案原样;布局现代:datacheck 汇总卡为视觉主角 + 覆盖主表,期权新鲜度/覆盖 Tabs,行情明细右侧面板双栏强化。

## Constraints

- 只动 `web/src/pages/data/**` + 自己的 html 壳 + 页面级 vitest;共享层(src/{api,hooks,lib,components})**只读**(KlineChart/ChartBase 按 props 消费)
- PRIVACY 红线:永不调 /v1/futu/quote
- TS strict 零 any;注释 ≤1 行;不引入新依赖
- bars 表单契约不变(端点/参数与旧版一致);30s 轮询 + visibilitychange;周期 Tabs
- 深链无需新语义(如旧版有保留)

## Links

- Driven-By: doc/tasks/2026-08-11-react-refactor.md(切片 P3);Sol 评估:datacheck 保持汇总卡+明细表形态
- Checklist: doc/tasks/2026-08-11-react-refactor-checklist.md(Data 14 项)
- 规范: doc/FRONTEND.md;共享层契约: web/src/api/types.ts
- Branch: `feat/slice3-data`(worktree `.claude/worktrees/slice3-data`)
- 执行者: Claude coder subagent

## State

- **status**: `reviewed_pending_merge`(2026-08-11)
- **last step**: reviewer A 有条件通过(feature);2 条 P1 已修(327d78e:补数据 stopPropagation + 防误触断言;Page.test.tsx TS2740);tsc 0 错 / vitest 32/32 / build 绿;52711f0 + 327d78e 待合入

## Next

- 合入 main(顺序 watchlist → data → results → admin);遗留观察:P3 观察 1 共享类型 `DatacheckItem.latest` 与服务端 max_ts 不符(切片 5 修共享层);P3 观察 2 覆盖表用原始 antd Table 组合(合法);reviewer 排期建议:CI frontend job 补 `npx tsc --noEmit`(本次 TS2740 漏网)
