# 文档：数据管道总览 + 调度方式（-every vs 外部 cron）

- **id**: `2026-07-31-doc-data-pipeline-scheduling`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

v1 数据管道收尾文档：新增 `doc/DATA_PIPELINE.md`（命令一览、行为保证、`-every` vs 外部 cron 调度选择与示例），并加入 `doc/README.md` 索引。

## Constraints

- 不改代码；纯文档。

## Links

- [[ROADMAP]] v1
- 前置：`doc/tasks/2026-07-31-ingest-time-range.md`（已完成）
- Driven-By: /loop 循环中主会话按计划优先级 ④ 拆出

## State

- **status**: `done`
- **last step**: 新建 `doc/DATA_PIPELINE.md`（命令/flags、校验/可重复/失败容忍/事务保证、调度方式对比表、cron 示例）；`doc/README.md` 索引加链接。

## Next

- commit + push + CI 绿即闭环。后续候选：数据源 Provider 抽象（需设计，建议拆解为讨论或 Issue 后再做）。
