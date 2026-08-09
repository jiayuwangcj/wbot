# datacheck 交易所节假日日历抽象

- **id**: `2026-08-09-datacheck-market-calendar`
- **created**: `2026-08-09`
- **updated**: `2026-08-09`

## Goal

在现有市场时区/周末判断上增加可注入交易日历，让港股、美股、沪深在休市日不被误判为 stale，也不触发无意义补拉。

## Constraints

- 依赖 P0；不阻塞 P1 只读观察面。
- calendar 必须可注入、可离线测试；默认数据源和更新方式另行小设计拍板。
- 不把第三方网络日历变成 serve 启动硬依赖；失败时保留安全降级。
- 覆盖周末、法定休市、半日市/收盘时间、美国 DST 边界测试。

## Links

- Driven-By / trigger: `doc/tasks/2026-08-07-daily-data-completeness.md`
- PR / branch: P0 后新建

## State

- **status**: `queued`
- **last step**: 已识别当前 weekday-only 的剩余风险并限定解决范围。

## Next

P0 后先写 calendar interface 与测试矩阵设计，不直接引入在线依赖。
