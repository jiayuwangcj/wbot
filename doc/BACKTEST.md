# Wheel 回测契约

回测必须复用动态 Wheel 的库存语义：满仓价、清仓价、最大库存、战略状态、有效库存和战术门槛均来自完整的版本化 Wheel 配置。回测只记录 bar-time 的信号/机械结算 trace，不发送订单。当前运行器以 bars 为时间轴，从 `option_quote_snapshots` 选择 `observed_at <= bar.ts` 的最新原子批次（同一时点按 `snapshot_key` 稳定选择），再调用 Wheel；它不是按 quote 或成交事件驱动的历史执行回放。缺少可信 snapshot 时，运行固定为 `DATA_BLOCKED/HOLD`，不可用日线收盘、固定 Greeks 或默认盘口“跑通”。

## 能力状态

| 能力 | 状态 | 阻塞原因 | 启用闸门 | 禁止降级 |
| --- | --- | --- | --- | --- |
| Wheel 领域决策（曲线插值、库存缺口、状态、候选风控） | `READY` | P0-A 单测已通过 | 保持确定性单测和回归样例 | 不把回测状态当作实时提醒 |
| bar-time 最新原子 snapshot 回放 | `READY`（研究/验证） | 已实现按 bar 选择最新批次；它不提供事件级成交时序 | 同输入同 trace、批次不混用、过期 → HOLD 的回归证据 | 不宣称实时执行或事件驱动历史回测 |
| 真实供应商 adapter / 实时 Wheel 输入 | `DATA_BLOCKED` | 尚无经过验收的供应商 adapter 提供同一时点完整 bid/ask、Delta、IV、Theta、OI、volume、lot size 和 freshness | 字段映射、真实只读采样、原子性、断线/限流/陈旧测试通过 | 不用日线收盘、固定 Delta/IV/OI、默认 Theta 或拼接不同时间数据 |
| 历史事件 Wheel 回测 | `DATA_BLOCKED` | 历史覆盖不足以还原逐 quote/成交事件及同一时点盘口/Greeks | 历史 snapshot 覆盖目标日期/DTE，事件 trace 可复现并通过质量验收 | 不用 OHLC 猜 bid/ask/Greeks，不把 bar-time 回放冒充事件回测 |
| 覆盖率/参数面研究 | `RESEARCH_ONLY` | 依赖事件回测和足够历史快照 | P1-A 完成、滚动窗口和可复现 seed 验收 | 研究结果不写 `ALERT`，不改写用户配置 |
| 期货等价库存/保证金 | `INTEGRATION_BLOCKED` | 乘数、实时 Delta、币种和券商规则未接通 | 账户/合约元数据与边界测试验收 | 不估算保证金，不把缺失期货数据视为零 |
| 回测详情浏览器验证 | `READY` | Mac Chrome desktop/390px、5-row 动态 trace、批量模式和 rerun 参数回填已验证 | 保持 DOM/CDP 与前端契约回归 | 不把 bar-time UI 冒充事件回测或人工写动作 |

`DATA_BLOCKED`、`INTEGRATION_BLOCKED`、`RESEARCH_ONLY` 和 `OUT_OF_SCOPE` 都是产品状态，不是可由默认值绕过的错误。每次运行应保留 `capability_status`、`blocked_by`、缺失字段和下一启用条件。

## 命令边界

CLI 入口保留确定性运行器参数，但产品策略名只有 `wheel`：

```bash
wbot backtest \
  -dsn "$WBOT_PG_DSN" -symbol HK.00700 -timeframe 1d \
  -strategy wheel \
  -params '{"full_position_price":400,"zero_position_price":550,"max_inventory":1200,"move_interval_pct":0.018,"min_premium_per_share":1.2,"stock_switch_pct":0.03,"trade_gap":50,"min_dte":5,"max_dte":10,"min_option_quality":0.6,"strategic_state":"NORMAL"}'
```

| flag | 默认 | 说明 |
| --- | --- | --- |
| `-file` / `-dsn` | — | 输入二选一；Wheel 需要 DB 中的期权快照，bars 文件不能产生 `ALERT` |
| `-symbol` / `-timeframe` | `DEMO.US` / `1d` | DB 输入的标的和周期 |
| `-symbols` | 空 | 多标的仅用于内部 benchmark；不支持 Wheel 的期权快照语义 |
| `-from` / `-to` | 不限 | RFC3339 时间范围（DB 输入） |
| `-limit` | 10000 | DB 输入最大 bars 数 |
| `-cash` | 10000 | 初始现金（>0） |
| `-strategy` | `hold` | CLI 实际默认是内部 `hold` 基准；显式 `-strategy wheel` 才运行 Wheel。产品 API/watchlist 只接受 `wheel` |
| `-params` | — | `wheel` 新配置必须提供 `full_position_price`、`zero_position_price` 与 `max_inventory`；百分比用小数；旧曲线仅兼容读取；内部 `hold`/`buy-hold` 不接参数 |
| `-fee` | 0 | 回测费用占位；不改变提醒契约 |
| `-max-drawdown` | 0 | 结果约束（0..1）；超限退出 1 |
| `-save` | false | 保存 metrics、完整 `strategy_params`、equity/trades/signals trace；要求 `-dsn` |
| `-export` / `-format` | 0 / `csv` | 导出已保存结果，格式为 `csv` 或 `json` |

`hold`/`buy-hold` 由 CLI 底层运行器保留为内部 benchmark；CLI 默认就是 `hold`，但它们不是 Wheel 策略，不出现在 `/v1/strategies` 或 `/v1/watchlist`，产品 API 也拒绝它们。旧策略名称只在迁移审计中保留，不得作为新配置或新 watchlist 请求。

## Wheel 配置和事件

`params` 是单一配置对象，至少包含：

```json
{
  "strategy": "wheel",
  "params": {
    "full_position_price": 400,
    "zero_position_price": 550,
    "max_inventory": 1200,
    "move_interval_pct": 0.018,
    "min_premium_per_share": 1.2,
    "stock_switch_pct": 0.03,
    "trade_gap": 50,
    "min_dte": 5,
    "max_dte": 10,
    "min_option_quality": 0.6,
    "strategic_state": "NORMAL"
  }
}
```

满仓价必须为正、清仓价必须更高、最大库存为正整数；两价之间从满仓到零仓线性插值、区间外钳制。百分比字段使用小数（`0.018 = 1.8%`），新战术字段省略时默认为 0 并关闭对应行为。策略无每日次数限制；报告可保留每日提醒/成交次数统计。库存事件至少记录：`stock_shares`、`futures_equivalent_shares`、`option_delta_stock`、`actual_inventory`、`effective_inventory`、`target_inventory`、`inventory_gap`。有效库存为实际库存加带符号期权 Delta；空 Put 增加、空 Call 减少有效库存。

每个事件必须保留原子快照标识、配置版本、候选列表、拒绝原因和动作。动作只有：

- `ALERT`：快照完整、能力状态为 `READY`、候选通过全部硬门槛，供人工评估；
- `HOLD`：数据不完整、状态不允许、库存缺口在无交易区间，或所有候选被拒绝。

候选至少需要 `expiry`、`strike`、`delta`、`bid`、`ask`、`implied_vol`、`theta`、`volume`、`open_interest`、`lot_size`、`observed_at`、`source`。缺任一字段、盘口过期/倒挂、零流动性、DTE 越界、质量分不足或风险约束失败，必须写入拒绝原因并保持 `HOLD`。

## 当前 trace 语义与事件级阻塞

当前每根 bar 的顺序是：读取 bar → 选择截至该 bar 时点的最新原子 snapshot → 运行 Wheel → 在 bar close 机械结算 → 写入 equity/trade/signal trace。snapshot loader 的 `limit` 表示完整批次数，不在 SQL 行级截断多合约批次；若设置开始时间，还会向前读取一个配置 freshness 窗口，使首根 bar 能使用仍新鲜的前置 snapshot。snapshot 不会跨批次拼接，未来时间的 snapshot 不会泄漏到当前 bar；没有所需方向的 `observed_at <= bar.ts` 可信批次时，Wheel signal 为 `DATA_BLOCKED/HOLD`。普通风险限制产生的 HOLD 保持 `capability_status=READY`，两者不可混为一类。这是一种可复现的 bar-time replay，不是事件驱动回测。

事件驱动的到期、指派、人工确认和成交回填指标仍是 `DATA_BLOCKED`：它们需要覆盖目标日期/DTE 的完整历史 snapshot、quote/成交事件顺序和人工审计事实。已保留 snapshot schema、runner trace、机械到期结算和 deterministic fixtures；解锁证据必须包含供应商/历史数据字段映射、原子性/新鲜度测试、端到端可复现 trace 和最大库存违规为零。禁止用 OHLC、固定 Delta、默认流动性、事后中间价或 bar-time replay 冒充事件证据。

## 指标与 trace

事件回测完成后，若数据闸门已启用，至少报告：总收益、最大回撤、指派率、Call 被行权机会成本、订单/提醒频率、库存偏差和最大库存违规数。trace 至少区分：信号、未执行信号、人工确认、人工回填成交、到期和指派；系统不生成 broker order id，也不调用交易 API。

参数研究只允许在离线数据上改变 DTE、候选映射、质量门槛、频率和覆盖率（100%、固定覆盖、随机漏 30%/50%，随机种子可复现）。曲线、最大库存、战略状态和资产配置不参与优化。

## CLI/API 一致性与导出

`wbot backtest -save`、`GET /v1/backtests`、`GET /v1/backtests/{id}` 和 export 共用同一落库 trace。详情包含 `equity_curve`、`trades` 和逐 bar `signals`；运行参数包含完整 `strategy_params`。新 trace 同时保存 `capability_status`、`blocked_by`、`snapshot_key`、`snapshot_observed_at`、实际/有效库存和期权 Delta 库存，UI 与 CSV/JSON 导出均保留这些审计字段。人工动作与 watchlist `config_version` 属于独立审计表，尚未接入回测详情，不得宣称已包含。导出时间统一 RFC3339 UTC `Z`。服务端执行超时、数据缺失或阻塞时应返回可执行的 `code/message/action`，不能把阻塞伪装为成功。

## 实现对账

- `internal/wheel`：两价区间、库存、状态、候选校验和 `ALERT/HOLD` 决策；P0-A `READY`。
- `internal/strategy`：唯一注册项 `wheel`；bar-time 适配器只消费当前最新原子 snapshot，缺失/过期时固定产生 `DATA_BLOCKED/HOLD`。
- `internal/wheelstore`、`internal/watchlist` 与 migrations 005/007：版本配置、不可变 snapshot、signal/action 审计表、`READY/DATA_BLOCKED` 数据库约束和 watchlist 版本指针；P0-B/P0-C repository 与 PostgreSQL integration `READY`，真实供应商 adapter 与人工写动作仍受阻塞。
- `internal/backtest` / `internal/backtestexec` 与 `internal/db/migrations/006_backtest_signals.sql`：确定性 bar-time replay、完整策略输入、逐 bar signal 保存/导出路径；不能替代事件级 Wheel 快照回测或实时执行。

关联：[[API]] [[DATA_PIPELINE]] [[WHEEL_STRATEGY]] [[ROADMAP]]
