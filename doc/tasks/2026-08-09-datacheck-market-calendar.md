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

- **status**: `done`
- **last step**: 已落地离线可注入 calendar、2026 官方休市/半日市数据、370 天防挂死降级与周末/节假日/半日市/DST 回归测试。

## Next

进入 P3 缺失报告外部通知；calendar 年度更新只需替换内建日期，运行时不增加网络依赖。

## Decision / Evidence

- `Policy.Calendar` 接受自定义实现；nil 使用 `ExchangeCalendar`，默认路径完全离线。
- 2026 沪深休市以 SSE/SZSE 年度通知为准；HKEX 覆盖 14 个工作日休市与 3 个半日市；NYSE 覆盖 10 个休市日与 2 个 13:00 提前收市日。
- 正常日延续收盘后 30 分钟缓冲：沪深 15:30、港美 16:30；港股半日 12:30、NYSE 提前收市 13:30。
- 非 2026 年安全降级到 market-local 周一至周五；错误注入日历连续 370 天无交易日时自动退回内建日历，避免报告挂死。
- 官方来源：
  - SSE: <https://www.sse.com.cn/disclosure/announcement/general/c/c_20251222_10802507.shtml>
  - SZSE: <https://www.szse.cn/disclosure/notice/t20251222_618087.html>
  - HKEX: <https://www.hkex.com.hk/-/media/HKEX-Market/Services/Circulars-and-Notices/Participant-and-Members-Circulars/SEHK/2025/ce_SEHK_CT_075_2025.pdf>
  - NYSE: <https://www.nyse.com/markets/hours-calendars>
- `go test ./internal/datacheck -count=1`: PASS。
