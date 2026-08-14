# 2026-08-12 订阅最小使用(按市场时段订阅/退订 + 上限管理)

- **id**: `2026-08-12-session-subscription`
- **created**: `2026-08-12`
- **parent**: 老板指令 2026-08-12(futu 网关订阅有上限,基于最小使用为原则:收盘了就不再订阅;晚上美股开盘后运行美股模拟盘)

## Goal

futu 网关订阅数有上限。按「最小使用」管理:仅交易时段订阅对应市场 symbols,收盘/非交易时段不订阅(并退订已订阅项);wheel runner 按市场时段评估,盘外不调 quote/subscribe;避免订阅数只增不减顶到网关上限。

## 现状(已探明)

- wheel runner 是纯 interval 定时循环,**无市场时段判断**——全天评估所有 watchlist symbols(盘后仍 subscribe+quote)
- futu client Quote() 每次调用都 `subscribe(is_sub_or_un_sub: true)`——订阅只增不减(SubType_Basic)
- option_quote.go 有独立 subscribe(合约级)
- 今日美股已开跑:US.JD 已入 watchlist(31.25 曲线 30→33,DATA_BLOCKED 等期权行情)
- datacheck 有市场日历能力(可复用判断交易日/休市)

## 设计(拟)

1. **市场时段定义**:港股 09:30-16:00 HKT(午休 12:00-13:00 跳过评估);美股夏令时 21:30-04:00 HKT、冬令时 22:30-05:00(跨日期);周末/休市日不评估(复用 datacheck 市场日历或内置规则)
2. **runner 门控**:Run 循环内按 symbol 市场+时段判断,盘外跳过评估(不调 Quote/不订阅);时段切换边界(开盘首轮订阅、收盘退订)
3. **退订**:futu client 加 Unsubscribe(或 Quote 支持 un_sub);收盘时对当日订阅过的 symbols 执行 `is_sub_or_un_sub: false`
4. **订阅簿跟踪**:serve 内存维护「本时段已订阅」集合,退订/重订幂等
5. **配置**:时段可配(wbot.conf 或 serve.env),默认值如上

## Constraints

- 交易时段内行为零变化(评估/推送/确认不受影响)
- 退订失败不阻断评估(日志告警即可);重复订阅幂等
- 涉及内部/futu(client/option_quote)、internal/wheelrun(runner 门控)——与进行中任务无文件重叠(push-ui/order-expiry/assistant 都动 cmd/wbot/discord_scheduler.go,本任务动 runner 侧,可并行;但合入串行)
- verify.sh 全绿;署名按实际编写模型

## Links

- 市场日历:datacheck(2026-08-09 任务)
- 美股模拟盘:US.JD watchlist 已入(2026-08-12)
- 订阅现状:internal/futu/client.go:92(Quote 每次 subscribe)

## State

- **status**: `in_progress`(2026-08-12 老板再次强调:「为何晚上美股时间要等待港股,不合理,后续优化」→ 优先级提升)
- **last step**: 实测实证(2026-08-12 22:2x):pass 卡在 `option-quotes: snapshot [30/86]` Post 超时——每轮 5-6 分钟是各标的串行拉期权行情(限频+超时)拖出,盘外标的照拉不误;美股时段(21:30+ HKT)先跑已收盘港股 → US.JD(ORDER BY symbol 末位)被拖慢
- **排期**:提升至 LLM 策略落地(#37)之前或并列(美股模拟盘实测主线依赖;当前 US.JD 每轮等待 ~6 分钟不可持续)

## Next

- 细化设计(时段表/退订接口/订阅簿)→ 派 codex(优先于 #37;与 LLM 策略落地文件重叠面:internal/wheelrun/runner.go —— 串行合入)
- 快速先做项(并入本任务):时段门控 runner——盘外标的跳过 Quote/期权拉取(港股 16:00 后、美股 04:00 后不再拉行情);美股时段只评估 US.JD(港股跳过)→ pass 时间从 ~6 分钟降到秒级
