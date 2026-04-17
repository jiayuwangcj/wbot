# 调整路线图：数据优先于回测与模拟盘

- **id**: `2026-04-18-roadmap-data-first-priority`
- **created**: `2026-04-18`
- **updated**: `2026-04-18`

## Goal

将公共计划与 [[ROADMAP]] 对齐：**先数据拉取与落地，再回测；模拟盘/执行深化延后**（无数据则回测无意义）。

## Constraints

- 不删除既有代码；仅调整文档叙述与任务优先级。

## Links

- [[ROADMAP]]

## State

- **status**: `done`
- **last step**: 更新 `doc/ROADMAP.md`、`doc/proposals/0001-automation-baseline.md` Follow-ups、issue 草稿说明；新增本任务记录。

## Next

- 新开或排队一条 **`queued`/`running`** 任务：数据管道第一刀（例如：符号与时间范围约定、落盘布局、mock 数据源 + 测试）。
