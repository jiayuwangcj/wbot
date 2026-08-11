# React 重构切片 5:收尾(契约强化 + 遗留修复 + 体积记录)

- **id**: `2026-08-11-react-p5-finalize`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

四片(P1-P4)已全部合入 main(93c9d21)。收尾:删残留占位、契约强化 grep、共享层遗留修复、CI 补 tsc、体积记录。视觉走查与 accept 复跑为环境依赖项(主会话/CI 负责)。

## Constraints

- **本片允许动共享层**(P3 观察 1 标注「切片 5 修共享层」):web/src/{api,types.ts} 类型修复;其余共享层仍谨慎,改动需过 CI
- 红线不变:永不调 /v1/futu/quote;admin 配置只写不读(值绝不回显);零外链(BotFather 豁免)
- 无原生 [skip ci];TS strict 零 any;不引入新依赖
- 体积记录写入 doc/FRONTEND.md(每页入口 kB + gzip,含共享 chunk 现状)
- CI 补 `npx tsc --noEmit`(reviewer 排期建议,本次 TS2740 漏网教训);scripts/verify.sh 同步补

## Links

- Driven-By: doc/tasks/2026-08-11-react-refactor.md(切片 5)+ reviewer 排期建议 + P3/P2 观察
- Checklist: doc/tasks/2026-08-11-react-refactor-checklist.md(全站 7 项)
- 规范: doc/FRONTEND.md;flake 排期: doc/tasks/2026-08-11-flake-limiter-crossprocess.md(不属本片)
- Branch: `feat/slice5-finalize`(worktree `.claude/worktrees/slice5-finalize`)
- 执行者: codex(gpt-5.6-luna max)

## State

- **status**: `reviewed_pending_merge`(2026-08-11 完成)
- **last step**: codex 提交 `5dae465`(fix(web): align datacheck contract and type checks,署名 gpt-5.6-luna,未 push);占位残留 0;DatacheckItem.max_ts 类型修复+data 页 hack 回退;CI/verify.sh 补 tsc --noEmit;dist 红线通过;体积基线写 FRONTEND.md(入口均 <800kB,共享 antd 897.95kB/图表 162.73kB);自测全绿(Vitest 76、Go 全量、verify.sh)
- **评审(2026-08-11)**:reviewer 结论「通过,无 P0/P1,建议合入」,功能类型 **bugfix**(契约对齐+工具补强)。实测:tsc exit 0、vitest 76 全绿、体积 14 项逐一对账、dist 红线零命中、check-skip 合规(web/scripts/.github 不在白名单→全量执行)。
- **评审非阻塞发现(排期 follow-up)**:[P2] verify.sh 前端段补 `npm run test`(本地门禁与 CI 不对等,单行,但 verify.sh 是关键脚本须走正常编码流程);[P3] ci.yml tsc 移至 build 前;[P3] types.ts 删 `DatacheckReport.complete` 幽灵字段(Go Report 无此 JSON 字段,前端零消费)
- **pending**: PR → CI → 合入 main(合入后删 4 个已合入切片 worktree)

## Next

- 评审通过 → 开 PR 合入 main → React 重构整体收口
- 手动项(合入后提示用户或随环境具备时执行):主题视觉走查(浅色/两套深色 × 5 页)、深链 e2e 冒烟、ACCEPTANCE.md 14 脚本复跑(CI db-integration 已含 accept-wheel-live)
