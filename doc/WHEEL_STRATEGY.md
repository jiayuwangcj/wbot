# 动态 Wheel / 库存管理策略

本文是 `wbot_wheel_strategy_spec_v1.docx` 的工程化基线。系统只保留一个库存驱动的 `wheel` 策略；旧 `covered-call` 与 `cash-secured-put` 模板退出产品与 API，不承担兼容职责。

## 1. 边界

- 战略层由人定义价格—目标库存曲线、最大库存和状态，不由回测优化。
- 战术层根据目标库存与有效库存之差，只选择 5–10 DTE 的 Put 或 Call。
- 执行层只产生提醒与日志；LLM 审核通过后，Telegram 人工确认仅允许模拟环境下单，任何情况下都不自动下单。
- 技术指标、方向预测、宏观预测和单点参数寻优不进入 v1。

## 2. 统一配置

每个标的使用一份结构化配置：

```json
{
  "strategy": "wheel",
  "params": {
    "price_position_curve": [
      {"price": 400, "target_inventory": 1200},
      {"price": 480, "target_inventory": 600},
      {"price": 550, "target_inventory": 0}
    ],
    "max_inventory": 1200,
    "lot_size": 100,
    "min_dte": 5,
    "max_dte": 10,
    "min_option_quality": 0.6,
    "max_daily_orders": 1,
    "extreme_max_daily_orders": 2,
    "no_trade_gap": 50,
    "strategic_state": "NORMAL"
  }
}
```

约束：曲线价格严格递增，目标库存单调不增并位于 `[0, max_inventory]`；DTE 范围有效；质量分在 `[0,1]`；正常日最多 1 张，极端日硬上限 2 张。价格落在锚点之间时线性插值，区间外钳制到端点。

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
target_inventory    = interpolate(price_position_curve, current_price)
inventory_gap       = target_inventory - effective_inventory
```

合约数量使用带符号约定：多头为正、空头为负；市场 Delta 使用标准符号。由此空 Put 贡献正库存，空 Call 贡献负库存。

决策规则：

- `gap > no_trade_gap`：只扫描 Put。
- `gap < -no_trade_gap`：只扫描 Call。
- 其余：`HOLD`，写入不交易原因。

## 4. 候选与风控

候选快照必须包含 `expiry/strike/delta/bid/ask/implied_vol/theta/volume/open_interest/lot_size/observed_at/source`。缺字段、过期报价、倒挂盘口或零流动性的候选直接淘汰，不使用日线收盘价冒充实时盘口。

质量分由价差、成交量、未平仓量、权利金/行权价、IV 和绝对 Theta 共同组成；Delta 通过“交易后有效库存距离”进入首要排序，避免重复计权。`min_option_quality` 是硬门槛。通过门槛后按以下稳定顺序排序：

1. 交易后有效库存更接近目标；
2. 质量分更高；
3. 价差更窄；
4. 到期日、行权价和代码作确定性排序。

每个候选必须同时通过：

- Put 指派后实际库存不超过 `max_inventory`，且现金检查覆盖所有已有空 Put 的行权价×张数×lot 承诺，再加本次候选；不能只检查新的一张；
- Call 指派后不会形成裸空头；
- 开仓后的有效库存不比开仓前更偏离目标；
- 当日次数未达到正常/极端上限；
- 战略状态允许该方向。

输出包含方向、数量、报价、质量分、交易前后库存、指派后库存、触发理由和全部拒绝理由。输出的唯一动作是 `ALERT` 或 `HOLD`。

正常日硬上限 1 张已由领域层执行；第二张只允许在外部已确认“显著二次价格/库存偏移”的极端日上下文中评估。当前产品尚未接入可审计的二次偏移判定，因此不会自行把普通日升级为极端日；该触发器在证据完成前保持 `INTEGRATION_BLOCKED`。

## 5. 数据与持久化

已有日线期权记录不足以产生可执行提醒。当前落库采用不可变报价 snapshot，并为配置、信号和人工处置分别留痕：

- `wheel_configs`：版本化战略配置与状态。
- `option_quote_snapshots`：盘口、Greeks、OI 与采集时间。
- `wheel_signals`：包含 `ALERT/HOLD`、候选、库存快照、理由和配置版本。
- `wheel_signal_actions`：LLM `LLM_REVIEW`、Telegram 人工确认/忽略/拒绝/成交或备注；系统自身不自动下单。
- `wheel_signal_dismissals`：按 symbol 与 UTC 当日记录 Telegram 的“今日不再提醒”。
- `backtest_results.signals`（migration 006）：保存逐 bar 的 `ALERT/HOLD` 决策轨迹，包括 `capability_status`、`blocked_by`、原子 `snapshot_key/observed_at` 和库存分解；回测输入同时保存完整 `strategy_params`，用于复现。
- 只读审计 API：`GET /v1/wheel/configs`、`GET /v1/wheel/signals`、`GET /v1/wheel/signals/{id}/actions`。不提供写动作，不具备身份/授权闭环。

历史 watchlist 参数迁移采用显式失效：旧策略行标记为需要重新配置，不猜测价格曲线或最大库存。

### 5.1 实时链路与人工闸门

`serve -wheel-run` 启动后，runner 按 `-wheel-interval` 为每个 Wheel watchlist 绑定读取当前价、账户持仓、期权链和同一批次合约报价，执行库存曲线与风险校验，并 append-only 写入 `wheel_signals`、同步 watchlist execution status。任一依赖失败或报价字段不完整都落 `HOLD`/`DATA_BLOCKED`，不生成可执行提醒。

完整快照产生 `ALERT` 后，runner 使用 `$LLM_BASE_URL`、`$LLM_API_KEY`、`$LLM_MODEL` 调用 OpenAI-compatible 审核器；审核结果以 `LLM_REVIEW` action 保存，只有 `APPROVE` 才进入 Telegram 推送，`REJECT` 或调用失败保持 fail-closed。缺少任一 env 时 serve 启动告警，ALERT 不会静默地伪装成已推送。

`serve -telegram-run` 读取配置的 token/chat ID，向允许的 chat 推送带 `yes`、`no`、`今日不再提醒` 的按钮。`yes` 由人工触发且只允许 sim 环境，失败或 real 环境写 `REJECTED`；`no` 写 `NO`；dismiss 写入当日静默表并抑制同一 symbol 当日后续提醒。Telegram loop 与 Wheel runner 独立启停。

## 6. 回测与验收

当前回测不是逐事件状态机，而是确定性的 bar-time replay：按 bars 升序处理，每根 bar 只选择 `observed_at <= bar.ts` 的最新原子 snapshot（同一时点按 `snapshot_key` 稳定选择），然后运行 Wheel 并在 bar close 机械结算。loader 的 `limit` 按完整批次数而非合约行数计数，并向首根 bar 前回看一个 freshness 窗口，因此既不会截断多合约批次，也不会漏掉仍新鲜的前置快照。它不把不同 snapshot 拼接，也不把未来报价泄漏到当前 bar；没有所需 Put/Call 方向的完整新鲜 snapshot 时只能 `DATA_BLOCKED/HOLD`。这条路径可用于研究和契约验证，但不等价于事件驱动历史执行。

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
| 价格曲线、库存计算、状态和风险决策 | `READY`（P0-A） | `go test ./internal/wheel` 通过；保持单测与确定性样例 |
| 唯一 `wheel` 注册表、required schema、watchlist 校验 | `READY`（P0-B） | registry/watchlist 单测和 API 契约回归通过；旧名称明确拒绝 |
| 版本配置、不可变 snapshot、signal/action repository | `READY`（P0-C） | migration、repository、fail-closed 测试及真实 PostgreSQL integration 已通过 |
| bar-time 最新原子 snapshot 回放 | `READY`（研究/验证） | 同输入同 trace；每根 bar 取截至该时点的最新批次 |
| Web 结构化配置、只读信号和回测 trace | `READY` | Mac Chrome desktop/390px 与动态 DOM 断言通过；人工动作仍只读 |
| 极端日第二张触发判定 | `INTEGRATION_BLOCKED` | 领域硬上限已实现；显著二次价格/库存偏移的可信事件输入和审计规则尚未接入 |
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

仅审核 wheel 策略；信号只能是 ALERT 或 HOLD；审核不得触发自动下单；候选必须有完整、及时的期权报价；不得超过最大库存、每日订单数或战略状态限制；数据不足时必须拒绝。
