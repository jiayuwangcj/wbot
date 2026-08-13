# 2026-08-13 LLM 策略定时运行(#37)

- **id**: `2026-08-13-llm-strategy-scheduler`
- **parent**: #37(LLM 策略定时运行,deepseek-v4-flash,每 15 分钟)
- **coder**: sol(gpt-5.6-sol,medium 思考,2026-08-13 老板指令:本任务用 sol 不用 luna)

## Goal

LLM 策略按固定节奏定时运行:每 15 分钟对 watchlist 标的生成策略决策信号,复用 #35 的注入链路与审核闸门,决策结果推送与 wheel 闭环一致。

**前置(2026-08-13 老板指令「全面 review 后开发」,sol review 报告 P1 先决项,必须在自动调度落地前完成)**:

1. **P1-1 交易确认幂等**(telegram_scheduler.go:403 / discord_scheduler.go:652 / store.go:789):Telegram 与 Discord 两渠道 check-then-act 竞态可重复下单;下单成功但 AppendAction(CONFIRM) 失败后重试可再次下单。修法:数据库级原子 claim/幂等键(不能只加进程内 mutex),两渠道共用同一确认服务或同一原子操作。
2. **P1-2 信号创建边界**(llm_signal.go:206-228):先 AppendSignal 后补库存只改内存不改库,审计/推送读的是错误快照。修法:先采集行情+账户+库存完整上下文并确定性验证,再一次性构造不可变 SignalRecord 落库。
3. **P1-3 确定性 LLM 输出校验器**:硬约束(数量≤上限、premium/current_price>0、expiry 未过期且与合约一致、Delta 符号/范围、strike 数量级、现金担保/备兑/库存/每日限额)由确定性代码校验,LLM 审核只作附加防线;不合格输出记为 generation rejection,不得写 READY/ALERT。
4. **P1-4 禁止 expiry fallback**(llm_signal.go:313):`syntheticOptionCode` 缺 expiry 时固定 260821 必须改为明确拒绝。
5. **P1-5 调度幂等与恢复契约(最小可用版)**:生成前查 DB 同 symbol 最近 15 分钟(或同 time bucket)是否已有未处置信号,有则跳过;不依赖内存 map;重启/重试不得重复注入。完整 run ID/lease 契约可后续迭代,但「DB 查询去重」是本任务底线。

## Constraints

- **定时周期 15 分钟**;与 serve 同生命周期(runCtx),可开关/可配置(参考现有 wheel 循环的启动方式)。
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
