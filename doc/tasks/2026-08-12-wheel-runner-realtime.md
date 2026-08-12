# 2026-08-12 wheel runner 实时性优化(ATM 中心扩展拉取 + 评估/记录分离 + 时段门控)

- **id**: `2026-08-12-wheel-runner-realtime`
- **created**: `2026-08-12`
- **parent**: 老板指令 2026-08-12(①「为何晚上美股时间要等待港股,不合理,后续优化」②「拉取期权行情不需要全拉,从中间点往外两边拉取即可,如果不是存储数据则可以很快拉到需要的,另外实盘运行期间机会稍纵即逝,不应该处理离线任务该做的事情,至于数据记录,另外开协程单独做」)

## Goal

wheel runner 实盘评估**实时化**:pass 从 ~6 分钟降到秒级。三管齐下:

1. **时段门控**:盘外标的跳过评估(港股收盘后/美股时段只评估美股;反向亦然)——不再让已收盘标的拖慢活跃标的
2. **ATM 中心扩展拉取**:期权行情不全量拉——以现价为中心(ATM)向两侧(ITM/OTM)交替扩展,拉到「评估够用的候选」即停;评估不需要全链数据
3. **评估/记录分离**:实盘评估循环走快速路径(只做决策所需),数据记录(全量快照/审计/历史)由独立协程异步落库,不阻塞评估

## 现状(已探明,2026-08-12 实测)

- runner 每 pass 串行评估所有 watchlist 标的:Quote → Positions → OptionChain(全量 DTE 窗口合约,US.JD=86 个)→ OptionQuotes(全量分批 snapshot,30/批,实测第 30 个 10s 超时 + 逐合约 greeks 1 req/3s 限频)→ Evaluate → AppendSignal → reviewAlert → SetExecutionStatus
- 实测卡点:22:2x `option-quotes: snapshot [30/86]: context deadline exceeded`;每 pass 5-6 分钟,US.JD(ORDER BY symbol 末位)被拖在最后
- OptionQuotes 全量拉取:`internal/futu/option_quote.go:146`(quoteBatch=30 分批)+ 逐合约 greeks(1 req/3s)
- 无市场时段判断(runner 纯 interval 循环,盘外照常 quote/订阅)
- 落库(信号/快照)在主评估路径内同步执行

## 设计(拟)

### 1. 时段门控(评估只跑活跃市场)

- 市场时段表:港股 09:30-16:00 HKT(午休跳过)、美股夏令时 21:30-04:00 HKT;周末/休市日跳过
- runner 循环内按 symbol 市场判断:盘外标的跳过 Quote/Chain/OptionQuotes(不调网关、不订阅)
- 依赖 datacheck 市场日历(已有)或内置规则;时段可配

### 2. ATM 中心扩展拉取

- Chain 拉取后按 strike 排序;现价定位 ATM(最近 strike)
- 从 ATM 向两侧交替扩展(ITM/OTM 各 1 档、2 档…),每轮扩展拉行情,直到:候选已够(如 ≥2 个通过质量校验)或达到扩展上限(±N 档,如 ±10)→ **不等全链**
- 评估语义:wheel 只需「当前最优候选」,不需要 86 个合约全景——全链数据只有离线/存储场景才需要
- OptionQuotes 增加「按子集拉取」能力(或 runner 层先筛子集再调);greeks 同样只拉子集
- 保持 Evaluate/Validate 决策逻辑不变(输入变少但语义一致)

### 3. 评估/记录分离

- 评估循环内:**同步路径只做决策**(行情子集 → Evaluate → 决策 → 信号记录(必留,审核/推送依赖));全量数据记录(行情快照、审计明细)入异步队列
- 独立协程从队列消费落库(批量、失败重试/告警,不阻塞评估);队列有界(溢出降级为丢记录+日志,不拖垮评估)
- 注意:wheel_signals 落库是审核/推送的依赖(信号 ID 关联),保持同步;快照/明细类可异步

## Constraints

- wheel.Evaluate/Validate 决策逻辑与输出契约不变(候选从子集来,质量校验/方向语义不变)
- wheel_signals 信号记录保持同步落库(审核/推送链路依赖);异步化的只有非阻塞性记录
- 探活/订阅纪律不变(不 SIGQUIT;退订幂等);verify.sh 全绿
- 文件重叠:internal/wheelrun/runner.go(与 LLM 策略落地 #37 串行合入)、internal/futu/option_quote.go
- 与 session-subscription(订阅管理:退订/订阅簿)互补:本任务做时段门控+拉取优化,订阅簿退订归该任务,串行合入

## Links

- session-subscription(2026-08-12,订阅最小使用/退订,互补任务)
- internal/futu/option_quote.go:146(snapshot 30/批实测超时)、runner.go:150(全量 chain)
- datacheck 市场日历(时段判断复用)
- memory serve-troubleshoot-discipline(探活纪律)

## State

- **status**: `done`
- **last step**: 已完成 ATM 中心扩展、交易时段门控和异步快照记录；`scripts/verify.sh` 全绿，待 reviewer 判定后合入/发布
- **2026-08-13 实测收口**:US.JD 实盘链路全通(453→456 逐步打通,456 为 **LLM APPROVE + Discord ✅ 下单按钮**);新增 4 个 P0 修复(超时/时区/审核输入/新鲜度)与 1 个 DB 参数变更,见下方「实测补充(2026-08-13 凌晨)」

## Implementation / Evidence

- runner 将完整 option chain 按唯一 strike 排序并以现价定位 ATM，按 ATM、下侧、上侧交替扩展；只对新增层调用 `OptionQuotes`，达到 2 个同方向且通过 `wheel.Validate`/最低质量阈值的候选即停止，最多中心 ±10 层。
- 过滤持仓仍使用完整 option chain，`wheel.Evaluate`/`Validate` 未修改；信号 append、LLM gate 和 watchlist 状态同步保留在决策路径。
- 默认离线复用 `datacheck.ExchangeCalendar`，按 HKEX/NYSE 本地时区、午休、周末、节假日和美股 DST 门控；盘外标的在 Quote/Chain/OptionQuotes 前跳过。
- option quote snapshot 通过可选 recorder 的 64 批有界后台队列写入 `option_quote_snapshots`；队列满时丢批并记录 stderr，不阻塞评估。
- 回归覆盖 ATM 交替/上限、按层子集请求、港股午休/美股夏令时与休市、盘外零网关调用和 recorder 异步不阻塞；`PATH=/home/jiayu/go/bin:$PATH scripts/verify.sh` 输出 `verify: ok`。

## 实测补充(2026-08-12 22:32,派单前实证)

- signal 433 US.JD:actual=0 ✓(库存修复生效)、gap=+1005.5 ✓(卖 PUT 方向)、**candidates=[]** ← 行情全量拉取超时(`snapshot [30/86]` 10s)→ 无候选 → HOLD → 推送未产生
- 美股盘中(22:32,开盘 22:30)评估轮空 = 「机会稍纵即逝」的直接实证:全量 86 合约拉取是唯一阻塞点
- 新一轮 pass(434,22:38)又从头跑 4 个已收盘港股 → 时段门控必要性再次实证

## 实测补充(2026-08-13 凌晨,实盘全链路打通记录)

**信号轨迹**:441 HOLD(旧版配额)→ 447/448 HOLD(429 被拒占配额,已修 dailyOrders 只计 LLM_REVIEW)→ 449/450 HOLD(质量 0.6 过严,DB 调 0.5)→ 451 HOLD(现金缺失,已接 Funds)→ 453 ALERT(资金接入生效)→ 454/455 ALERT(LLM 拒:审核输入问题)→ 456 ALERT **LLM APPROVE → Discord ✅ 下单按钮**(00:28 HKT)。

**本轮 4 个 P0 修复(均 commit 于 feat/llm-signal-endpoint)**:
1. **LLM 审核超时 60s→180s**(7cb6e44):deepseek-v4-pro 实测 168s;超时降级 REJECT 是 fail-closed 安全行为,但 60s 每次都拒。同时推送标题区分「⚠️ 审核失败」(details 有 error)与「❌ 被拒绝」。
2. **US 期权 update_time 时区错误**(e195c0d):futu 网关对 US 返回**美东时间**(HK/CN 为 +08),parseQuoteTime 统一按 +08 → 盘中新鲜报价(12:14 ET 时 HKT 00:19)被标 12 小时前 → LLM 以「数据陈旧」拒。修复:market==11 用 America/New_York。HK 对比验证:00700 update_time 16:07:58(收盘后 +08 合理)。
3. **审核输入补全**(e195c0d):cash_available 未传给 LLM 审核(Funds 只接了 Evaluate);信号无现价字段;OptionQuote 遗留 ts/timestamp/captured_at 零值时间序列化噪音(Go time.Time omitempty 无效)→ ReviewRequest 加 CurrentPrice、reviewAlert 传现金、userContent 序列化后清理零值时间(a288ded 修 dropZeroTimes 对 struct 无效问题)。
4. **max_quote_age 默认 24h 太宽**(DB 变更):31.5P 报价 1h43m 前仍过 Validate 且被选中(库存目标优先排序)→ US.JD 配置加 `max_quote_age: 3600`,旧报价被 Validate 拒,新鲜报价(31P)入选。

**性能曲线**:全量 5-6 分钟 → ATM 扩展冷缓存 4-5 分钟 → 缓存命中 28 秒 → 方向过滤 + 新容器首轮 30 秒。

**残留观察项**:
- deepseek-v4-pro 审核 168s 接近 180s 上限,若上游再慢会再次 fail-closed;考虑审核异步化或提高超时(产品决策)
- Evaluate 选中逻辑按「距目标库存最近」优先,报价新鲜度只做 Validate 门槛——阈值与排序的权衡已在配置层解决(1h),若低流动性合约频繁旧报价,可考虑排序加分

## Next

- 细化设计(时段表/扩展档位上限/异步队列契约)→ 派 codex(待老板批准;LLM 策略落地 #37 之前)→ 快速先做项:ATM 扩展拉取(改 OptionQuotes 子集能力 + runner 选子集)单点即可解除「全量超时无候选」
- 验证口径:美股时段 pass 秒级 + US.JD 评估不再等港股/不再全量拉取;verify.sh 全绿;评审功能类型 bugfix(性能/正确性修复)
