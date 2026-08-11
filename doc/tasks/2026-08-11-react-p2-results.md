# React 重构 P2:results 页迁移

- **id**: `2026-08-11-react-p2-results`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

把 `web/src/pages/results/` 从占位页迁移为完整功能(React + TS strict,现代简洁不平铺)。

验收 = checklist `doc/tasks/2026-08-11-react-refactor-checklist.md` **Results 18 项逐项等价**,含空态文案原样;布局现代:结果列表(搜索+分页)+ 详情(指标卡 + 权益曲线主图 + Tabs:交易/信号轨迹/参数)。

## Constraints

- 只动 `web/src/pages/results/**` + 自己的 html 壳 + 页面级 vitest;共享层(src/{api,hooks,lib,components})**只读**(WheelForm 按 props 契约消费)
- PRIVACY 红线:永不调 /v1/futu/quote
- TS strict 零 any;注释 ≤1 行;不引入新依赖
- 深链 `#bt-<id>` 保留;**对比选择跨分页保留**(多选状态页面级提升,翻页丢选是产品 bug)
- CSV/JSON 导出直链(URL 与旧版一致);一键回测跳转(watchlist → results#bt-)

## Links

- Driven-By: doc/tasks/2026-08-11-react-refactor.md(切片 P2);Sol 评估验收项 1-10(对比跨分页)
- Checklist: doc/tasks/2026-08-11-react-refactor-checklist.md(Results 18 项)
- 规范: doc/FRONTEND.md;共享层契约: web/src/api/types.ts
- Branch: `feat/slice2-results`(worktree `.claude/worktrees/slice2-results`)
- 执行者: Claude coder subagent

## State

- **status**: `reviewed_pending_merge`(2026-08-11)
- **last step**: reviewer B **通过**(feature,无 P0/P1);18 项核对 17/18 等价达标(第 7 项「加载更多」以 antd 分页实现,交互微差列 P2/P3);共享层零改动、红线干净、无 secrets/skip;f7ded51 待合入

## Next

- 合入 main(顺序 watchlist → data → results → admin);排期跟进 P2×2:openDetail 竞态请求序号守卫(Page.tsx:125-142)、分页 hasMore 语义替换拼凑 total(Page.tsx:424);P3 记录:失败态详情卡缺返回按钮、NaN ts 过滤、results 新增 30s 轮询属行为增量(交付说明注明);遗留观察:共享 antd chunk >500kB(切片 5)
