# 文档：回测用法（doc/BACKTEST.md）

- **id**: `2026-07-31-doc-backtest`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

固化 v2 回测现状：新增 `doc/BACKTEST.md`（命令/flags/指标/约束/示例/实现指针），`doc/README.md` 索引加链接。

## Constraints

- 纯文档；不改代码。

## Links

- [[ROADMAP]] v2
- 前置：`doc/tasks/2026-07-31-backtest-constraint.md`（已完成）
- Driven-By: /loop 循环主会话按计划优先级 ④ 拆出

## State

- **status**: `done`
- **last step**: 新建 `doc/BACKTEST.md`；`doc/README.md` 索引加 [[BACKTEST]]。

## Next

- commit + push + CI 绿即闭环。后续：多 symbol 时间对齐（需设计拆解，写草稿）。
