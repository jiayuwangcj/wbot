# 前端 React + TypeScript 重构(现代简洁 UI + 并行派发友好)

- **id**: `2026-08-11-react-refactor`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

老板 2026-08-11 三条指令:
1. 为后续更复杂前端,**重构为 React**,寻找成熟库复用,尽量减少代码
2. **TypeScript strict**,规范项目自起草(`doc/FRONTEND.md`);架构方便**后续并行任务派发**
3. **页面简洁美观、不平铺功能、现代网页**(信息层级化,指标卡+图表为视觉主角,表格/表单收纳抽屉/Tabs/折叠;参照 2026-08-02 UI Goal:富途/IB/嘉信)

执行案:主会话技术计划 `/home/jiayu/.claude/plans/mutable-nibbling-music.md`(Vite MPA + antd5 + lightweight-charts 4.2.3 + TS strict;切片 0 基线→P1–P4 页面并行→切片 5 收尾)+ Sol 产品对比结论(6 项吸收,见计划文件「Sol 对比结论」节)。

## Constraints

- **硬前置**:切片 F 合入主基线后才派切片 0(老板指定流程:等 F → Sol 对比 → 执行;Sol 对比已完成)
- 页面切片文件零重叠(pages/<page>/** 私有;共享层 api/hooks/lib/components 切片 0 冻结,FRONTEND.md 所有权地图)
- 页面合入顺序按高频优先:watchlist → data → results → admin(≠编码顺序;占位页中间态显式声明)
- PRIVACY 红线:永不调 /v1/futu/quote;admin 配置只写不读;UI 走 Go embed(静态树/404/304 契约保持)
- 14 个 accept-*.sh 全绿不变是零破坏强校验;切片 5 须在 dist 之上复跑

## Links

- Plan: `/home/jiayu/.claude/plans/mutable-nibbling-music.md`(批准 + Sol 对比结论)
- Sol 产品评估: `doc/issues/draft-2026-08-11-react-refactor-sol-eval.md`(111 行;6 项吸收)
- UI Goal(2026-08-02): 记忆 project-goal-2026-08-02-ui(富途/IB/嘉信参照、主题化、持续演进)
- 现状事实: webui 5 页 + app.js 2703 行 + style.css 1016 行 + vendored lightweight-charts v4.2.3;webui_test.go 67 测试函数;embed webui.go:12 `web/*`
- 功能等价对照清单(已落盘 2026-08-11,提交 3385dca): `doc/tasks/2026-08-11-react-refactor-checklist.md`,实计 81 项(Dashboard 12/Watchlist 21/Results 18/Data 14/Admin 9/全站 7),逐项锚点+空态文案;P1-P4 每片验收核对单

## State

- **status**: `slice0-complete`(React 基线已在独立 worktree 完成并通过本地工程门禁)
- **last step**: 切片 0 已交付：Vite MPA、共享层、dashboard 参考页、dist embed、CI artifact 链路和构建脚本完成

## Next

1. reviewer 复核切片 0 的功能等价和 dist 构建链，随后按 watchlist → data → results → admin 顺序派发 P1–P4
2. P1–P4 并行派发(4 个 worktree,文件零重叠),共享层按冻结签名消费
3. 切片 5 收尾(契约强化/视觉走查/深链冒烟/体积记录)

## 切片 0 交付记录

- React 入口、共享 API/hooks/lib/components 和 dashboard 参考页已实现；其余页面明确显示「迁移中，切片 N 交付」。
- Go embed 已切换到 `web/dist/*`，旧 HTML/app.js/vendor 已删除；`style.css` 与 favicon 字节不变迁移到 `web/public/`。
- Node 构建已接入 dev-up、verify、release、Makefile 和 CI frontend artifact；Go test/db-integration 下载 artifact 后编译。
- 提交序列：`e2bcd10`、`4ff47e7`、`66d9c72`、`cbadb05`、`bf5bd7d`、`f930a4c`、`0d16c43`、`2fc5986`。
