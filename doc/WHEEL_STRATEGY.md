# 动态 Wheel / 库存管理策略

本文是 `wbot_wheel_strategy_spec_v1.docx` 的工程化基线。系统只保留一个库存驱动的 `wheel` 策略；旧 `covered-call` 与 `cash-secured-put` 模板退出产品与 API，不承担兼容职责。

## 1. 边界

- 战略层由人定义满仓价、清仓价、最大库存和状态，不由回测优化。
- 合约乘数（lot size）不接受配置：运行时从行情 `contract_size` 实时拉取，拉不到按 100 兜底；存量 `lot_size` 旧配置键迁移时忽略并累计告警（2026-08-13）。
- 战术层根据目标库存与有效库存之差，只选择 5–45 DTE 的 Put 或 Call（上限 2026-08-14 由 10 放宽：短窗口权利金薄、候选稀少，回测评估后放开）。
- 执行层只产生提醒与日志；LLM 审核通过后，Telegram 人工确认仅允许模拟环境下单，任何情况下都不自动下单。
- 技术指标、方向预测、宏观预测和单点参数寻优不进入 v1。

## 2. 统一配置

每个标的使用一份结构化配置：

```json
{
  "strategy": "wheel",
  "params": {
    "full_position_price": 400,
    "zero_position_price": 550,
    "max_inventory": 1200,
    "move_interval_pct": 0.018,
    "min_premium_per_share": 1.2,
    "min_option_profit": 200,
    "stock_switch_pct": 0.03,
    "covered_call_pct": 0.05,
    "trade_gap": 50,
    "min_dte": 5,
    "max_dte": 10,
    "min_option_quality": 0.6,
    "strategic_state": "NORMAL"
  }
}
```

约束：`full_position_price > 0`，`zero_position_price > full_position_price`，`max_inventory` 为正整数；DTE 范围有效，质量分和 `covered_call_pct` 在 `[0,1]`，其余战术参数非负。`covered_call_pct` 默认 0.05，covered call 行权价下限为 `max(现价×(1+covered_call_pct), 正股成本)`；存量配置省略该键时使用默认值。候选期权的 `expected_gain = Bid × 合约乘数 × 数量` 低于 `min_option_profit`（默认 200，单位 HKD/笔）时淘汰；它与 `min_premium_per_share` 同时生效，设为 0 可关闭总收益门槛。百分比 JSON/CLI 一律使用小数（`0.018` 表示 `1.8%`）；界面显示 `%` 时乘 100。两价之间按满仓到零仓线性插值并在区间外钳制；策略不设每日提醒次数上限。

战略状态：

- `NORMAL`：按库存缺口双向工作。
- `CAUTION`：允许减仓；增仓时降低数量并偏向更低 Put 行权价。
- `PAUSE_BUY`：禁止新增 Put，仍允许有覆盖的 Call。
- `EXIT`：禁止 Put，只允许库存退出方向的 Call。

## 3. 库存模型

```text
actual_inventory    = stock_shares + futures_equivalent_shares
option_delta_stock  = Σ(signed_contracts × market_delta × lot_size)
effective_inventory = actual_inventory + option_delta_stock
target_inventory    = interpolate(full_position_price, zero_position_price, max_inventory, current_price)
inventory_gap       = target_inventory - effective_inventory
```

合约数量使用带符号约定：多头为正、空头为负；市场 Delta 使用标准符号。由此空 Put 贡献正库存，空 Call 贡献负库存。

决策规则：

- `gap > trade_gap`：只扫描 Put。
- `gap < -trade_gap`：只扫描 Call。
- 其余：`HOLD`，写入不交易原因。

## 4. 候选与风控

候选快照必须包含 `expiry/strike/delta/bid/ask/implied_vol/theta/volume/open_interest/lot_size/observed_at/source`。缺字段、过期报价、倒挂盘口或零流动性的候选直接淘汰，不使用日线收盘价冒充实时盘口。HKEX 日终 `bid=ask=settlement` 投影只允许离线 `RESEARCH_ONLY` 回测，不进入本策略的实时提醒输入。

质量分由价差、成交量、未平仓量、权利金/行权价、IV 和绝对 Theta 共同组成；Delta 通过“交易后有效库存距离”进入首要排序，避免重复计权。`min_option_quality` 是硬门槛。通过门槛后按以下稳定顺序排序：

1. 交易后有效库存更接近目标；
2. 质量分更高；
3. 价差更窄；
4. 到期日、行权价和代码作确定性排序。

每个候选必须同时通过：

- 卖 Put 必须 `strike < 正股现价`，卖 Call 必须 `strike > 正股现价`；OTM 硬过滤先于质量和权利金排序；
- covered call 行权价必须达到配置的价外幅度并且不低于正股成本；
- Put 指派后实际库存不超过 `max_inventory`，且现金检查覆盖所有已有空 Put 的行权价×张数×lot 承诺，再加本次候选；不能只检查新的一张；
- Call 指派后不会形成裸空头；
- 开仓后的有效库存不比开仓前更偏离目标；
- 战略状态允许该方向。

正股直接卖出同样不得低于持仓平均成本；`stock_switch_pct` 在急跌时只 HOLD 并保留持仓，价格恢复到成本以上才允许卖出。等于成本的成交只容许手续费级损失。

输出包含方向、数量、报价、质量分、交易前后库存、指派后库存、触发理由和全部拒绝理由。输出的唯一动作是 `ALERT` 或 `HOLD`。

每次评估仍只生成一张待人工审核的合约建议，但后续有效评估不会因当日已产生提醒或成交而被抑制。每日提醒/成交次数可作为报告统计，不能作为领域限制。

## 5. 数据与持久化

已有日线期权记录不足以产生可执行提醒。当前落库采用不可变报价 snapshot，并为配置、信号和人工处置分别留痕：

- `wheel_configs`：版本化战略配置与状态。
- `option_quote_snapshots`：盘口、Greeks、OI 与采集时间。
- `option_quotes`：HKEX 官方日终 settlement/IV/成交事实；`source=hkex,adjust=none`，不等同实时 snapshot。
- `wheel_signals`：包含 `ALERT/HOLD`、候选、库存快照、理由和配置版本。
- `wheel_signal_actions`：LLM `LLM_REVIEW`、Telegram 人工确认/忽略/拒绝/成交或备注；系统自身不自动下单。
- `wheel_signal_dismissals`：按 symbol 与 UTC 当日记录 Telegram 的“今日不再提醒”。
- `backtest_results.signals`（migration 006）：保存逐 bar 的 `ALERT/HOLD` 决策轨迹，包括 `capability_status`、`blocked_by`、原子 `snapshot_key/observed_at` 和库存分解；回测输入同时保存完整 `strategy_params`，用于复现。
- 只读审计 API：`GET /v1/wheel/configs`、`GET /v1/wheel/signals`、`GET /v1/wheel/signals/{id}/actions`。不提供写动作，不具备身份/授权闭环。

存量参数按“新读旧、只写新”迁移：旧 `price_position_curve` 取首尾价格作为满仓价/清仓价，标记 `migration_lossy=true` 并保留原曲线审计 JSON；`no_trade_gap` 映射为 `trade_gap`；旧每日限额键和 `lot_size` 忽略并累计迁移告警。新写入只包含新参数键和迁移审计字段。

### 5.1 实时链路与人工闸门

`serve -wheel-run` 启动后，runner 按 `-wheel-interval` 为每个 Wheel watchlist 绑定读取当前价、账户持仓、期权链和同一批次合约报价，执行库存曲线与风险校验，并 append-only 写入 `wheel_signals`、同步 watchlist execution status。任一依赖失败或报价字段不完整都落 `HOLD`/`DATA_BLOCKED`，不生成可执行提醒。

完整快照产生 `ALERT` 后，runner 使用 `$LLM_BASE_URL`、`$LLM_API_KEY`、`$LLM_MODEL` 调用 OpenAI-compatible 审核器；审核结果以 `LLM_REVIEW` action 保存，只有 `APPROVE` 才进入 Telegram 推送，`REJECT` 或调用失败保持 fail-closed。缺少任一 env 时 serve 启动告警，ALERT 不会静默地伪装成已推送。

**异步审核（2026-08-14）**：审核在 runner 的 pass 循环外执行（有界队列 + 2 个 worker），单次审核耗时分钟级也不再阻塞其他标的的评估。同一标的已有审核在途时，后续 ALERT 不再重复入队（去重），推送闸门等待的审核已经在跑。

**重复候选抑制（2026-08-14）**：同一标的、同一合约在 `repeatAlertWindow`（30 分钟）内重复 ALERT 会降为 `HOLD`，不落 ALERT、不触发审核，窗口不滚动，期满后候选重新 ALERT 并重新审核。抑制基线只在 ALERT 落库成功后写入；**审核未产生业务结论时（LLM 调用失败、异常返回值、worker 异常、队列满丢弃）清除该标的基线**，下一 pass 重新审核——异步化前审核失败后下一 pass 的重复 ALERT 是隐式重试，现在由基线清除显式恢复，避免该标的通知静默最长 30 分钟。

`serve -telegram-run` 读取配置的 token/chat ID，向允许的 chat 推送带 `yes`、`no`、`今日不再提醒` 的按钮。`yes` 由人工触发且只允许 sim 环境，失败或 real 环境写 `REJECTED`；`no` 写 `NO`；dismiss 写入当日静默表并抑制同一 symbol 当日后续提醒。Telegram loop 与 Wheel runner 独立启停。

## 6. 回测与验收

当前回测不是逐事件状态机，而是确定性的 bar-time replay：按 bars 升序处理，每根 bar 只选择 `observed_at <= bar.ts` 的最新原子 snapshot；同一时点按 `futu` → `hkex` → 其他 source 字典序，再按 `snapshot_key`。loader 的 `limit` 按完整批次数而非合约行数计数，并向首根 bar 前回看一个 freshness 窗口，因此既不会截断多合约批次，也不会漏掉仍新鲜的前置快照。它不把不同 snapshot 拼接，也不把未来报价泄漏到当前 bar；没有所需 Put/Call 方向的完整新鲜 snapshot 时只能 `DATA_BLOCKED/HOLD`。HKEX 完整周期可输出带结算价投影风险标记的 `RESEARCH_ONLY` 模拟，但不等价于事件驱动历史执行。

事件驱动回测仍为 `DATA_BLOCKED`，需要完整历史 quote/成交事件、到期/指派时序和人工回填事实。已落库 snapshot schema、bar-time signal trace、机械到期结算和 deterministic fixtures；解锁证据必须包括历史覆盖、字段映射、原子性/新鲜度回归、端到端可复现 trace 和最大库存违规为零。禁止用 OHLC、固定 Greeks、默认流动性、事后价格或 bar-time replay 冒充事件证据。

事件级能力启用后，至少记录信号、未执行信号、人工确认、人工成交回填、到期和指派，并报告总收益、最大回撤、指派率、Call 被行权机会成本、订单频率、库存偏差和最大库存违规数。

参数研究只覆盖 DTE、候选映射、质量门槛、频率和信号覆盖率（100%、固定覆盖、随机漏 30%/50%）。采用滚动窗口和稳定平台判断，不输出单点“最优参数”。战略锚点、最大库存、资产配置及宏观状态不参与优化。

发布闸门：

- 任一输入字段缺失时安全地 `HOLD` 并给出原因；
- 任一路径不得触发交易 API；
- 单元、集成、迁移、前端契约和浏览器流程全部通过；
- 旧模板从注册表、页面、CLI 示例和文档中完全移除。

## 7. 能力状态与不可执行边界

文档、API 和页面必须使用下列状态，不允许只用“已开发”笼统描述：

| Status | 含义 | 产品行为 |
| --- | --- | --- |
| `READY` | 输入、实现和验收证据都齐全 | 可计算；仍然只发提醒 |
| `DATA_BLOCKED` | 逻辑已实现，但可信输入不存在或覆盖不足 | 返回 HOLD，并列出缺失字段 |
| `INTEGRATION_BLOCKED` | 领域接口已定义，外部网关/账户接口未接通 | 不显示为可操作能力 |
| `RESEARCH_ONLY` | 仅用于离线实验，不能驱动提醒 | 结果带研究标记，不写 ALERT |
| `OUT_OF_SCOPE` | 明确不建设 | 不留隐式开关或降级路径 |

当前基线：

| Capability | Status | 缺口 / 启用闸门 |
| --- | --- | --- |
| 两价区间、库存计算、状态和风险决策 | `READY`（P0-A） | `go test ./internal/wheel` 通过；保持单测与确定性样例 |
| 唯一 `wheel` 注册表、required schema、watchlist 校验 | `READY`（P0-B） | registry/watchlist 单测和 API 契约回归通过；旧名称明确拒绝 |
| 版本配置、不可变 snapshot、signal/action repository | `READY`（P0-C） | migration、repository、fail-closed 测试及真实 PostgreSQL integration 已通过 |
| bar-time 最新原子 snapshot 回放 | `READY`（研究/验证） | 同输入同 trace；每根 bar 取截至该时点的最新批次 |
| HKEX 日终 Wheel 回测 | `RESEARCH_ONLY` | 官方 settlement/IV/成交/OI + 派生 Greeks；无真实历史 bid/ask/成交事件，永不驱动提醒 |
| Web 结构化配置、只读信号和回测 trace | `READY` | Mac Chrome desktop/390px 与动态 DOM 断言通过；人工动作仍只读 |
| 真实供应商 adapter 与实时 Put/Call 提醒 | `DATA_BLOCKED` | 尚无验收过的可信源能提供同一时点完整 Delta、bid/ask、IV、OI、Theta、volume、lot size 和 freshness |
| 历史事件 Wheel 回测 | `DATA_BLOCKED` | 历史 snapshot/quote/成交事件覆盖不足，不能还原事件顺序 |
| 人工确认/忽略/成交回填 | `INTEGRATION_BLOCKED` | 需完成 Web/API 身份边界、权限和操作审计；仍不得自动下单 |
| 期货等价库存和保证金 | `INTEGRATION_BLOCKED` | 合约乘数、实时 Delta、账户保证金规则与币种换算来源明确 |
| 参数覆盖率/稳定平台研究 | `RESEARCH_ONLY` | P1 回测完成后启用；不得改写战略配置 |
| 实时/自动执行 | `OUT_OF_SCOPE` | 永久无交易 API 调用、无实时执行器、无自动确认开关 |
| 宏观/行业/技术指标预测器 | `OUT_OF_SCOPE` | 状态只允许人工切换并审计 |

当状态不是 `READY` 时，信号记录至少包含 `capability_status`、`blocked_by`、缺失字段和下一启用条件。前端不得用弱提示掩盖阻塞；候选区域显示明确的“数据不足，未生成建议”。

## 8. 外部数据接入契约

报价接入是可替换 adapter，不把供应商字段直接泄漏到领域层。一次原子快照必须满足：

- 同一 underlying、同一 `observed_at` 或在配置允许的最大时间偏差内；
- underlying price 与 option quote 的市场、币种和交易时段一致；
- Delta 在 Put `[-1,0]` / Call `[0,1]`，`0 < bid <= ask`，IV 非负，OI/volume 非负；
- 标注 source、source timestamp、ingested timestamp 和原始合约代码；
- 时钟漂移、部分合约失败和过期数据不能拼成完整快照。

Futu 接入未确认的字段或权限必须留在 `INTEGRATION_BLOCKED`，不得假定网关一定提供。接入完成的验收证据包括：字段映射测试、真实只读采样、快照原子性测试、断线/限流/陈旧数据测试以及零交易调用审计。

不可执行项登记：真实行情 adapter 的 retained work 是 domain quote DTO、`option_quote_snapshots` schema、缺字段留痕和 fail-closed `HOLD`；解锁需要可信只读 adapter、真实采样和上述全套测试，禁止固定 Delta/IV/OI/Theta 或跨时点拼接。人工 `CONFIRM`/`IGNORE`/`FILL`/`NOTE` 的 retained work 是 append-only action 表和 actor 字段，解锁需要身份、权限、审计和浏览器流程，禁止把确认当下单。期货腿的 retained work 是 futures-equivalent inventory 字段，解锁需要合约乘数、实时 Delta、保证金、币种换算和边界测试，禁止把缺失腿当零或估算保证金。实时/自动执行永久 `OUT_OF_SCOPE`，没有交易 client、实时执行器或隐式开关。

## 9. 发布与回滚

重构不迁移旧模板语义。数据库升级后，旧 watchlist 行保留原 JSON 作为审计证据并标记不可执行；用户保存完整 Wheel 配置后产生新版本。回滚只回滚应用读取路径，不删除新配置、快照、信号或人工动作历史。

每个发布批次在 `doc/tasks/2026-08-10-wheel-full-rewrite.md` 记录：提交、测试命令、浏览器证据、当前 capability status、尚未启用项和下一最小步骤。

## 10. LLM 审核规则摘要（单一来源）

> 此段为 LLM 审核规则唯一维护点，修改需同步 `internal/wheelrun/runner.go` 的 `wheelReviewRules` 常量。

仅审核 wheel 区间策略；信号只能是 ALERT 或 HOLD，审核不得触发自动下单。策略在满仓价到清仓价区间内线性计算目标库存，通过卖出现金担保 Put 或备兑 Call 收取权利金：目标库存高于有效库存时只能考虑卖 Put 增加库存敞口，目标库存低于有效库存时只能考虑卖 Call 降低库存敞口。
当前情况由标的现价、策略配置版本、现金可用额及股票/期权持仓组成；signal 描述提示动作、方向、卖出合约数、候选报价、当前/目标/有效/交易后库存、库存缺口、能力状态和阻断原因；预期收益 expected_gain 是按卖价 Bid × 合约乘数 × 数量估算的毛权利金，不含手续费、滑点、税费及指派损益，不代表保证收益，缺失或为零不得推断为有收益。
必须逐项审核：
1. 方向反转检查（硬性项）：核对 signal.direction 与当前持仓、effective_inventory、inventory_gap、target_inventory 及满仓价—清仓价区间一致；缺口为正时卖 Put、缺口为负时卖 Call，卖出/买入符号与目标库存变化必须一致，任何方向反转或符号矛盾都必须 REJECT。
2. 策略参数：核对 full_position_price/zero_position_price、max_inventory、move_interval_pct、min_premium_per_share、min_option_profit、stock_switch_pct、covered_call_pct、trade_gap、min_option_quality、min_dte/max_dte、strategic_state 及候选合约参数；卖 Put 必须 OTM，卖 Call 必须 OTM 且行权价不低于现价×(1+covered_call_pct)与正股成本。
3. 数据质量：报价必须完整且新鲜，Bid/Ask 正数且未倒挂，IV/Delta/Theta 合理，Volume/OI 非零；不得用缺失 Greeks 或过期、拼接数据作判断。
4. 资金与库存：核对现金/保证金预算、最大库存、Put 指派承诺、Call 备兑数量和交易后有效库存；策略不设每日提醒次数上限。
5. 系统性错误：排查闭市或停牌误判、同一合约重复动作、与现有持仓或历史动作矛盾、合约类型/到期日/乘数错误及 Greeks 缺失。
6. 数据不足：capability_status 为 DATA_BLOCKED、blocked_by 非空，或任一关键字段不足时必须 REJECT；不得以 expected_gain 补偿或放宽任何校验。
7. 改单（signal.replace 非空，硬性项）：改单=撤销 pending_orders 中旧挂单（replace.order_id/replace.contract）改挂首选候选，是写操作、同样需要审核。必须核对：a) 新合约不要求严格优于旧合约：允许价格稍差的调整（如权利金略低、质量相当），若理由合理——更快成交、流动性更好、更接近目标库存——应予批准；但新合约明显劣化（质量/流动性显著更差、风险显著增大）或调整无任何依据时必须 REJECT；b) 旧挂单确在 pending_orders 中且方向一致；c) 改单后库存偏差不增大；d) 频繁改单（同标的短时多次）必须 REJECT——避免反复撤换浪费与不确定性。
