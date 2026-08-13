# 2026-08-13 LLM 策略定时运行(#37)

- **id**: `2026-08-13-llm-strategy-scheduler`
- **parent**: #37(LLM 策略定时运行,deepseek-v4-flash,每 15 分钟)
- **coder**: sol(gpt-5.6-sol,medium 思考,2026-08-13 老板指令:本任务用 sol 不用 luna)

## Goal

LLM 策略按固定节奏定时运行:每 5 分钟对 watchlist 标的生成策略决策信号(2026-08-13 老板指令:15 分钟太久,改 5 分钟),复用 #35 的注入链路与审核闸门,决策结果推送与 wheel 闭环一致。

**前置(2026-08-13 老板指令「全面 review 后开发」,sol review 报告 P1 先决项,必须在自动调度落地前完成)**:

1. **P1-1 交易确认幂等**(telegram_scheduler.go:403 / discord_scheduler.go:652 / store.go:789):Telegram 与 Discord 两渠道 check-then-act 竞态可重复下单;下单成功但 AppendAction(CONFIRM) 失败后重试可再次下单。修法:数据库级原子 claim/幂等键(不能只加进程内 mutex),两渠道共用同一确认服务或同一原子操作。
2. **P1-2 信号创建边界**(llm_signal.go:206-228):先 AppendSignal 后补库存只改内存不改库,审计/推送读的是错误快照。修法:先采集行情+账户+库存完整上下文并确定性验证,再一次性构造不可变 SignalRecord 落库。
3. **P1-3 确定性 LLM 输出校验器**:硬约束(数量≤上限、premium/current_price>0、expiry 未过期且与合约一致、Delta 符号/范围、strike 数量级、现金担保/备兑/库存/每日限额)由确定性代码校验,LLM 审核只作附加防线;不合格输出记为 generation rejection,不得写 READY/ALERT。
4. **P1-4 禁止 expiry fallback**(llm_signal.go:313):`syntheticOptionCode` 缺 expiry 时固定 260821 必须改为明确拒绝。
5. **P1-5 调度幂等与恢复契约(最小可用版)**:生成前查 DB 同 symbol 最近 15 分钟(或同 time bucket)是否已有未处置信号,有则跳过;不依赖内存 map;重启/重试不得重复注入。完整 run ID/lease 契约可后续迭代,但「DB 查询去重」是本任务底线。

## Constraints

- **定时周期 5 分钟(老板指令 2026-08-13 从 15 分钟改为 5 分钟)**;与 serve 同生命周期(runCtx),可开关/可配置(参考现有 wheel 循环的启动方式)。
- **模型**:deepseek-v4-flash,走现有 LLM 链路(LLM_BASE_URL / LLM_API_KEY / LLM_MODEL 配置,serve 已有)。
- **复用 #36 策略接口抽象**(strategy 注册表,现有 "wheel" 模板,本任务落地 "llm" 模板或等价调度路径);不绕过抽象另起炉灶。
- **复用 #35 注入契约与审核闸门**:`POST /v1/wheel/llm-signal` 输入契约(symbol/direction/quantity/contract/current_price/premium/delta/iv/open_interest/expiry/reason/notes),LLM 审核 6 规则(方向一致性/经济理由/限价/数量≤1000/数据一致性/系统性)。
- **提示词**:优先采用 `doc/tasks/2026-08-12-llm-prompt-framework.md` 的固化框架思路(骨架固定 + 参数插槽 + 输出契约);若框架落地的提示词模板代码尚未存在,以 #35 现有 LLM 调用路径的提示词为基线扩展,框架独立演进不阻塞本任务。
- **数据源**:行情快照与 wheel 同源(同一行情/期权数据链路),决策可对照;不新建重复数据通道。
- **幂等/防重**:定时触发不得重复注入同一时刻的同一标的决策(DB 查询去重,见 Goal 前置 5);运行失败可重试但不堆积。
- 所有策略仅限价单(price>0 fail-closed);测试 fixture 用假值;敏感配置不进仓库。
- `scripts/verify.sh` 全绿才提交;提交署名 `Co-Authored-By: gpt-5.6-sol <noreply@openai.com>`(sol 编码时按实际模型署名——本任务由 codex exec -m gpt-5.6-sol 执行)。

## Links

- #37 任务(LLM 策略定时运行)
- `doc/issues/draft-2026-08-13-sol-review.md`(sol 全面 review 报告,P1 先决项出处,2026-08-13)
- `doc/tasks/2026-08-12-llm-prompt-framework.md`(提示词框架固化,Goal/现状已探明)
- `doc/tasks/2026-08-12-llm-signal-p1-fixes.md`(#35 收口记录:注入端点 + 审核闸门 + 推送闭环)
- `doc/tasks/2026-08-12-slice-a-shared-pipeline.md`(#36 策略接口抽象,strategy 注册表)
- 代码:cmd/wbot/llm_signal.go(#35 端点)、strategy 抽象(#36 注册表)、wheel 循环(wheel-run 的定时模式参考)

## State

- **status**: `done`
- **implementation**: P1-2/P1-4 → P1-3 → P1-1 全部先完成；新增 `llm` 策略模板、`serve -llm-run -llm-interval 15m`、固定 `deepseek-v4-flash` 生成客户端、共享注入/审核/推送链路与 DB 最近 15 分钟未处置信号去重。
- **safety**: migration 011 的 `wheel_order_claims(signal_id PK)` 在 broker 调用前原子 claim，Telegram/Discord 共用；broker 成功后即使 `CONFIRM` 审计追加失败，重试仍被数据库 claim 拒绝。确定性校验在 ALERT 落库前覆盖数量/正限价/expiry+contract/Delta/strike/行情一致性/现金担保/备兑库存/每日限额；失败仅记录 `HOLD/DATA_BLOCKED` generation rejection。
- **verify**: `scripts/verify.sh` 全绿（frontend/gofmt/test/vet/race/staticcheck/五目标交叉编译/CLI smoke/accept）；当前环境未设置 `WBOT_PG_DSN`，仓库既有 PG integration tests 按约定 skip，已补 migration 011 的真实 PG claim/去重断言供配置 DSN 时执行。

## Next

1. 转 reviewer 独立评审并判定 feature/bugfix。
2. 有可用 `WBOT_PG_DSN` 的环境补跑 `go test ./internal/wheelstore -run Integration -count=1`。
3. 运维以 `serve -llm-run -llm-interval 15m -telegram-run` 启动并观察真实 watchlist 的生成、推送、人工确认闭环。

## 实跑进展(2026-08-13 下午)

- **实盘信号**:631(HOLD/DATA_BLOCKED 首轮 generation rejection)、637(首个 ALERT:SELL 2 股,审核 REJECTED——决策/理由不一致)、648/653(LLM 信号 ALERT,审核 REJECTED:「数据不全」)。
- **审核输入补全(f81ed8c)**:老板判断「数据不全=程序 bug」。648 审核 4 条拒绝理由逐条对应缺失:inventory(effective/target/gap)、observed_options(期权链快照)、current_date(无法验证 DTE)、规则未声明数据范围(模型按 wheel 参数集核对 llm 策略)。修复:ReviewRequest 加 Inventory/ObservedOptions/AsOf;Submit 填充;ObservedOption 补 json tag;optionRules/stockRules 声明「缺失即不存在,不得因缺字段拒绝」。
- **agent 框架合入(53bde40)**:codex 02ab64c 合入主分支,generationPrompt 合并 agent 版 + direction 枚举规则(P1-1),P2-2 option_chain DTE 过滤/P2-3 工具描述对齐/P2-4 工具调用日志一并落地。
- **round 超时修复(7d5649a)**:信号 668 与 2026-08-13 15:20 tick 同签名失败——round=1 工具调用成功,round 2(带工具结果出最终决策)在 30s 每轮超时被掐断。模型推理最终决策超 30s,非偶发。修复:每轮超时 30s→90s(roundCtx 派生自 totalCtx,180s 总预算仍封顶)。已重建部署。
- **部署**:已重建 serve 容器(2026-08-13 15:00/15:22 两次),等待下一 llm tick 验证审核输入、agent 工具调用与 round 2 决策。

## 实跑闭环与推送式样(2026-08-13 晚)

- **首次全链路闭环(信号 687,07:41 UTC)**:agent 工具调用→决策(卖出 PUT 430,premium 3.115,delta -0.255,OI 403)→确定性校验→审核门 APPROVE(4 条理由:方向一致/参数合规/数据质量按声明范围/资金覆盖)→带按钮卡片推送→**用户按 ✅ 下单 → CONFIRM(discord)→ FILL(system:watch)模拟盘成交**。目标「看到 llm 策略实际跑起来并且推送下单」达成。
- **692(07:46)**:LLM_REVIEW APPROVE,带按钮推送待用户处置。
- **695(07:54)**:REJECTED——**策略性拒绝**(SELL 100 股会破坏 2 张短 Call 的备兑覆盖 → 裸露 Call 风险),审核判断合理,非程序 bug;notes 给了处置建议。
- **「数据不全=程序 bug」闭环(4 次修复)**:674 规则声明缺失(方向语义+target 镜像非硬约束,ac56d0f)→ 679 生成 JSON 围栏(parseDecisionJSON 剥围栏,b3fa93b)→ 684 systemPrompt 数据范围界定(57cdbfd)→ 输入补全+声明(前面 f81ed8c)。之后无数据类误拒。
- **拒绝卡与通过卡式样统一(b0c3696)**:老板指令「拒绝单和通过单式样不统一,以审核成功的单子为准,只是底部LLM审核区内容不一并且没有按钮」。alertCard 统一(✅ APPROVE/❌ REJECT/⚠️ 审核失败标签),拒绝卡无按钮;Discord 理由移至**最后一条 embed**(IM 注意力在末尾,老板指令)。
- **策略来源徽标(af2a88e)**:老板指令「单子未标明是大模型策略还是固化策略生成的」。wheel_signals 加 strategy 列(migration 010,默认 wheel);llmsignal 写 'llm'、wheelrun 写 'wheel';telegram/discord 卡片标题加徽标(🤖 LLM 策略 / ⚙️ 固化策略)。历史数据回填:有 LLM_REVIEW/REJECTED 动作且 details#>>'{input_summary,decision}' 存在 = llmsignal(13 条);wheelrun HOLD/ALERT 保持 wheel。回填键与 wheelrun 审核请求结构区分(Summary 无 decision 键)。
- **孤儿 ALERT 观察项**:审核在 Submit 内联执行,容器重建会打断 → 无重试机制(654 模式)。本次重建前均等待审核落库。backlog:审核失败重试机制。
