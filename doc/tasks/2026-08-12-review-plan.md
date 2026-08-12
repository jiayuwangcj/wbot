# 2026-08-12 操作复盘 + 策略统一排期

- **id**: `2026-08-12-review-plan`
- **created**: `2026-08-12`
- **updated**: `2026-08-12`

## Goal

复盘 2026-08-12 盘中实测与推送功能的操作教训,并排期解决老板指令:① LLM 策略真正运行(定时 15 分钟,deepseek-v4-flash)② 统一 wheel/LLM 两种策略接口 ③ 写入 doc/WHEEL_STRATEGY.md 主文档。

## 一、今日成果(验证链已闭环)

### 1. 00700 wheel 模拟盘实测(任务 #29 延续)

盘中信号 269–272 构成完整验证链,审核闸门可靠:

| 信号 | 结果 | 含义 |
| --- | --- | --- |
| #269 | REJECT | 「决策理由缺乏经济依据」→ llmStockRules 规则 2(经济理由硬性项)正常工作 |
| #270 | REJECT | 「可用现金 0」→ 暴露 available_cash 网关 bug(见教训 ①);「已有 100 股应说加仓」语义检查正常 |
| #271 | REJECT | 「llm reviewer unavailable」→ 暴露 env 导出陷阱(见教训 ②) |
| #272 | **APPROVE → 推送 → 老板 ✅ → 限价单 → 成交** | 全流程闭环,仓位 100→200(100 = 早前 CLI 直连测试单,100 = 本次) |

### 2. 推送功能(老板指令:重要事件必须推送)

按钮点击/下单成功/成交/LLM 拒绝理由/忽略全部推送;推送含**信号编号**与**订单号**,老板可回溯时间线。已上线(serve 重启,2026-08-12 22:5x)。

### 3. inventory 真实持仓(老板指令:推送持仓不能是注入的全 0)

llm-signal 注入 inventory 为 nil 时,用实时账户持仓填充 ActualInventory/EffectiveInventory/TargetInventory(200 股)。

## 二、今日教训(实测坑,均已修复/固化)

1. **available_cash 恒为 0**(模拟网关实测)→ futu_account.go 回退 `cash − frozen_cash`(futu_account_test.go 已覆盖)。
2. **eval env 导出陷阱**:`eval "VAR=x"` 创建普通 shell 变量(不导出),子进程 environ 无 LLM 变量 → serve 里的 reviewer 不可用。修复:保留 `export` 关键字。**教训已固化到本会话后续派单姿势**(先 `/proc/<pid>/environ` 验证再注入)。
3. **网关地址漂移**:futu-opend-rs 重启后 192.168.215.2 → 192.168.217.2(wbot-net)。`~/.wbot/inspect.sh` 第 13 行已更新(bash -n 验证)。
4. **pkill 自伤**:pgrep -f 匹配外层 shell wrapper → 分步 kill 精确 pattern。
5. **CLI 查询订单也需 -price**(无查询模式)→ 查询走 HTTP /v1/futu/orders。

## 二.5、全天行为分析(老板指令:记录日志,分析是否都符合预期)

DB 实测(wheel_signals 239-274,UTC,北京时间 = +8):

| 时段(北京) | 信号 | 行为 | 判定 |
| --- | --- | --- | --- |
| 09:45-10:20 | 09988 ×6 DATA_BLOCKED | 数据校验不过 | ✅ fail-closed 正确 |
| 10:26 | 00700 READY | 有行情后正常评估 | ✅ |
| 10:31-11:43 | 00883 ×6 DATA_BLOCKED | **网关无 00883 期权合约**(chain 实测 call/put 空) | ✅ fail-closed,环境限制 |
| 10:36-10:39 | 00700 ALERT ×3(相同候选 460put@11.45) | 248 LLM REJECT / 249 老板 NO / 250 审核过未处置 | ⚠️ **重复 ALERT 未抑制** |
| 10:41-10:51 | daily order limit HOLD ×2 | 每日下单限生效 | ✅ |
| 10:56-11:05 | 全标的 DATA_BLOCKED | **网关故障窗口**(subscribe EOF→127.0.0.1 refused,地址漂移) | ✅ fail-closed,但 serve 无自愈 ⚠️ |
| 11:11-11:21 | 00700 ALERT(264/266/267) | 264 审核过+老板 3 次确认 place order failed;266 LLM REJECT;267 审核过+2 次失败 | ⚠️ 地址漂移期下单失败(已修) |
| 11:26-11:34 | LLM 注入 268-272 | 268 审核过未确认;269/270 LLM REJECT;271 reviewer unavailable;272 APPROVE→CONFIRM→成交 | ✅ 闭环 |
| 11:39+ | daily limit / DATA_BLOCKED | 收尾正常 | ✅ |

**期权可用性实测(老板指令:查看港股期权模拟账户是否可用)**:
- 00700 期权合约存在(43 个行权价,8/21 到期 TCH260821P370000 起),期权到期日/链接口正常
- CLI 模拟盘卖 1 张 `HK.TCH260821P460000 @ 11.45` → **被后端拒绝:`Can't trade this type of securities (backend_code=-1)`**
- **根因(非权限!)**:`GetTradeAccounts()` 返回全部账户,`TrdAcc.SimAccType` 字段区分账户类型——**Stock=1(不支持期权)、Option=2(仅期权)、Futures=3**;accID=0 默认解析到第一个模拟账户 1907141(SimAccType=Stock)→ 股票账户拒绝期权类型。**13477968 是港股期权模拟账户(SimAccType=Option)**。
- **修复**:新增 `AccountForSymbol(env, symbol, accID)`——accID=0 时按市场+证券类型自动解析(期权→期权账户,正股→对应市场股票账户,TrdMarketAuthList 匹配市场),无匹配 fail-closed;新 CLI `futu accounts` 列出全部账户与类型。**期权账户下单实测通过**(见下)。
- 00883 无合约与权限无关(数据覆盖问题)
- **已排除「老板需开通期权权限」的旧结论**——账户切换即解决

**结论**:36 条信号全天节奏均匀,5 分钟 ticker 稳定;fail-closed(数据不足不 ALERT)、daily order limit、LLM 审核闸门全部按设计工作。**不符合预期的 2 项**:① 同候选重复 ALERT(3 连/2 连推送)未抑制 ② 网关地址漂移时 serve 不自愈(8 次老板点击下单失败,靠人工重启修复)。

## 二.75、多模拟账户自动解析(老板指令:账户切换 + 美股支持)

**账户清单**(`wbot futu accounts`,simulate env,2026-08-12 实测):

| acc_id | SimAccType | 市场 | 用途 |
| --- | --- | --- | --- |
| 1907141 | stock(不支持期权) | [1] HK | 港股正股模拟 |
| 1907143 | stock | [3]/[21][22] CN | 沪深正股模拟 |
| 13477968 | **option(仅期权)** | [1] HK | **港股期权模拟** |
| 13477966 | stock | [2]/[11] US | 美股模拟 |
| 281756478875559548 | real env | [1][2] | 实盘(只读) |

**机制**:`TradeClient.AccountForSymbol(env, symbol, accID=0)` — ParseSymbol 取市场 → `IsOptionCode` 判期权 → 匹配 `TrdEnv + SimAccType(option vs not) + TrdMarketAuthList 含市场`;无匹配报错列出可用账户(fail-closed,宁可不交易不冒名)。接入面:CLI order/funds/position/ingest、telegram futuOrderPlacer(PlaceOrder/OrderStatus)、FutuAccounter(LLM 审核上下文)。**审核上下文与交易账户一致**——期权信号不再误读股票账户。

**验证状态**(已提交 3f9fe14,verify.sh 全绿):
- 单元测试:IsOptionCode(9 例)、AccountForSymbol(期权/正股/code-first/US/SH/SZ/显式 accID/fail-closed)
- 实盘下单:卖 3 张 `TCH260821P460000 @ 11.45` 全部成交(卖空 PUT = wheel 持仓,option 账户);买回平仓 buy 3 @ 13.00 已受理(Submitted,港交所午休 12:00-13:00 撮合暂停,午后验证成交)
- 信号注入:275(正股→1907141)/277(期权→13477968) 审核上下文零账户解析错误(serve log grep account context = 0)

## 三、排期(老板指令 2026-08-12:统一策略接口 + LLM 策略定时运行)

现状调研结论(Explore agent,2026-08-12):两种策略信号生成端独立(wheel=定时拉数据 `wheel.Evaluate`;LLM=外部注入),但**后半段管道高度重复**:

| 重复点 | wheel 侧 | LLM 侧 | 收敛目标 |
| --- | --- | --- | --- |
| LLMReviewer 接口 | wheelrun.LLMReviewer(runner.go:40) | httpapi.LLMReviewer(llm_signal.go:41) | 并入 llmreview.Reviewer 单一声明 |
| 审核-落库闸门 | reviewAlert(runner.go:239) | reviewLLMSignal(llm_signal.go:319) | 抽 recordLLMGate(Review→APPROVE 记 LLM_REVIEW/REJECT 记 REJECTED) |
| 信号写面 | SignalStore(wheelrun) | LLMSignalStore(httpapi) | 统一 SignalRepository(AppendSignal+AppendAction+只读) |
| 候选 DTO | Candidates []map[string]any 手写 JSON 往返 | 同上 | 强类型 Candidate/Quote |
| 推送/下单 | confirmOrder/firstCandidate | **已完全共享** | 不动 |

**统一抽象边界**:不硬抽「统一 Strategy 接口」(生成端一个被动一个主动,强行收敛会扭曲)。抽象落点 = **「信号 → 审核 → 推送 → 确认 → 限价单 → 成交监控」共享管道**;两种策略都产出相同的 SignalRecord/Candidates,管道对其一视同仁。

### 切片 A:共享管道抽象(任务 #36,约 1-2 人天,重构零行为变化)

1. LLMReviewer 接口收敛(两处声明 → llmreview 包单一声明,签名逐字相同,机械合并)
2. recordLLMGate 抽取(reviewAlert/reviewLLMSignal 骨架合一,rules 文本与 Signal 载荷作参数)
3. SignalRepository 归一(wheelTelegramStore + LLMSignalStore 合并成完整读接口,store 实现不变)
4. Candidate/Quote 强类型化(Candidates 构造/解码两点共用,去 map 往返)
**验收**:全测试绿(现 60+ 测试保护)+ 行为零变化 + reviewer 评审通过

### 切片 B:LLM 策略定时运行(任务 #37,约 1-2 人天)

1. **模型(B1 已实测,2026-08-12)**:策略生成用 deepseek-v4-flash(老板指定)。实测结论:
   - 默认(纯推理模式):`content` 恒空,只有 `reasoning_content`(叙述性文本,不可当 JSON 解析)——inspect.sh 注释与实测一致
   - **`"thinking":{"type":"disabled"}` 参数可关闭推理 → content 直接输出干净 JSON**(json_object 模式实测返回 `{"symbol":...}` 可解析)
   - **方案定稿**:生成调用带 `thinking: disabled` + `response_format: json_object`(llmreview 客户端需支持透传该参数);审核闸门保持 deepseek-v4-pro(现有逻辑不动,无需 thinking 参数)
2. **调度**:新 `llm_strategy.go` — 15 分钟 ticker(flag `-llm-strategy-interval`,默认 15m),首个周期立即跑一次;与 wheel runner 同构
3. **生成上下文**:watchlist(策略绑定)+ 现价 + 账户持仓/现金(复用 FutuAccounter)→ flash 生成建议(方向/数量/限价/理由),走 llmStockRules/llmSignalRules 校验
4. **防信号风暴**:同 symbol 今日已有同方向 ALERT 未处置 → 跳过;重复建议(方向+价位相似)去重;max_daily_orders 语义沿用
5. 产出 SignalRecord 落 store → 审核 → 推送 → 老板确认 → 限价单 → 成交监控(全复用现有管道)

### 切片 C:主文档(约 0.5 人天)

doc/WHEEL_STRATEGY.md 补第 11 章「LLM 大模型策略」(定时 15 分钟/flash 生成+pro 审核/注入端点契约/防风暴规则/处置闭环),与第 10 章审核规则并列;README/API.md 同步

### 执行顺序

A(共享层先稳定)→ B(新功能坐共享层)→ C(文档收尾)。A 与 B 文件重叠(llm_signal.go),串行不并行。当前推送修复+测试已全绿,先合入 feat/llm-signal-endpoint 收口 #35。

## Links

- Driven-By: 老板指令 2026-08-12(盘中微信消息流)
- Tasks: #35(LLM 注入端点+闭环,已完成注入端)、#36(接口统一)、#37(定时运行)、#38(本文档)
- PR: feat/llm-signal-endpoint(合入中)

## State

- **status**: `running`
- **last step**: 多模拟账户自动解析完成并提交(3f9fe14,verify.sh 全绿,serve 已部署);#35 推送+账户解析待 reviewer 评审合入;复盘文档更新(期权根因/账户表/验证状态)

## Next

- 午后验证买回单成交(option 账户 round-trip 闭环)→ 收口 #35(推送+账户解析合入 feat/llm-signal-endpoint)→ reviewer 评审 → 老板确认排期 → 切片 A → B → C
