# Wheel 实时运行切片 E:Telegram 实时提醒 + yes/no/dismiss 处置闭环

- **id**: `2026-08-11-wheel-live-slice-e`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

新包 `internal/telegram/`(或扩展 notify):`sendMessage`(text + InlineKeyboardMarkup yes/no/dismiss 按钮)+ `getUpdates` 长轮询(offset 推进)+ `answerCallbackQuery`。serve 集成:`--telegram-run`;长轮询协程处理 callback_query。提醒文案=简单下单说明(方向/数量/候选期权 行权·到期·bid-ask·Δ·IV·OI/现价/库存缺口/信号 id/LLM 审核 APPROVE)。回调处置:
- `yes` → 复核最新 LLM 审核(信号未过期且 APPROVE)→ `PlaceOrder(sim env, 期权 code, side=方向, qty=张数, price=0 market)` → AppendAction(CONFIRM+order id)→ Telegram 回执(订单号)
- `no` → AppendAction(NO)→ 不再动作
- `dismiss` → 新表 `wheel_signal_dismissals(symbol, dismiss_date, UNIQUE(symbol, dismiss_date))`(**迁移 009**——008 已被切片 D 的 LLM_REVIEW 约束迁移占用,plan 原预留 008 已更新)→ 当日该 symbol 静默(runner 推送前查表跳过)

**Telegram 配置面(用户 2026-08-11 增补):admin 向导 + `~/.wbot` 落盘**——复用现有体系(不新建配置基建):
- `internal/config/store.go` `WhitelistedKeys` 追加:`credentials.telegram.token`、`credentials.telegram.chat_ids`(逗号分隔白名单,group `credentials.telegram`);PUT `/v1/admin/config/credentials.telegram.token` 即落盘 `~/.wbot/wbot.conf`(JSON,0600,原子写,现有实现)。
- **隐藏关键信息**:admin GET 只回 set/updated_at 元数据、值永不回显(现有 Entry 语义);webui 提交框保存后清空、只显示「已配置」标记;表单输入框用 type=password 类。
- **简单向导**:webui admin 页新增「Telegram 接入向导」区块(分步):① 说明 + @BotFather 创建指引(link)② token 输入 + 保存 ③ chat_ids 输入 + 保存 ④ 保存后显示已配置状态(BotFather/获取 chat_id 提示)。复用现有配置写面逻辑(app.js:594 renderConfig/select 提交)。
- 消费侧:telegram/runner 用 `config.Store.Lookup("credentials.telegram.token"/"chat_ids")` 读原始值(不在 serveMux 外传 store:消费侧自行 `config.OpenDefault()`,wbot.conf 为 tmp+rename 原子写,跨实例并发读不撕裂——注释说明);env 作 fallback 可选,以 wbot.conf 为主。

## Constraints

- **不碰**其他切片文件:`internal/futu/`(切片 B)、`internal/wheelrun/`(切片 C)、`internal/llmreview/`(切片 D)。
- **实盘下单默认拒绝**:env=sim 才允许 PlaceOrder(real 拒绝并记录,AppendAction 记 REJECTED);`--wheel-env real` 且无 `--live-confirm` → 下单路径 fail-closed。
- 回调校验 `from.id` ∈ 白名单(credentials.telegram.chat_ids 解析,逗号分隔);不在白名单 → answerCallbackQuery 拒绝提示,不处置。
- yes 路径必须先复核 LLM 审核:信号未过期(如 created_at ≤ 10min)且最新 LLM_REVIEW action APPROVE,否则拒绝并回执说明。
- 长轮询:单协程,offset 推进,错误 sleep 重试不退出;不阻塞 serve。
- 通知发送复用 notify.Telegram token 配置通道(env),只新增交互面。
- 遵守 self-documenting-code(注释 ≤1 行)。

## Links

- Driven-By: 用户指令 2026-08-11「实时人工提醒,接入 telegram,有简单的下单说明并且有 yes or no 选项框给我选择, yes 下单, no 继续等待机会, dismiss 今日不再提醒」
- Plan: `/home/jiayu/.claude/plans/mutable-nibbling-music.md` 切片 E
- Branch: `feat/slice-e-telegram`(worktree `.claude/worktrees/slice-e-telegram`)
- Depends-On: 切片 C(runner 信号流)+ 切片 D(LLM 审核闸门)

## State

- **status**: `queued`
- **last step**: 主会话已探索:notify.Telegram 仅纯文本 Send(无 InlineKeyboard/getUpdates/callback);迁移 005 wheel_signal_actions 的 action CHECK 约束(切片 D 已扩展含 LLM_REVIEW);PlaceOrder(OrderRequest{Symbol, Side, Qty, Price},0=market,sim env)

## Next

C/D 合入后:在 worktree `.claude/worktrees/slice-e-telegram`(branch `feat/slice-e-telegram`,基于合入后 HEAD)实现 telegram 交互包 + 迁移 008(或新迁移)wheel_signal_dismissals + serve 集成 + 回调 handler 单测(fake telegram server:getUpdates/callback;yes/no/dismiss/未知用户/审核过期)→ `scripts/verify.sh` 等价自测 → 独立分支提交(push)→ 报告改动文件/测试结果/遗留问题。
