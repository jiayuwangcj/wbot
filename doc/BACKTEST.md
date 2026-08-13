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

### 富途历史期权数据裁决（2026-08-14）

富途 `GetHistoryKL` 只能回填 K 线，缺少同一时点的 bid/ask、Delta、IV、Theta 和 OI；`GetOptionQuote` 是无历史时间参数的当前快照；`GetOptionChain` 的日期参数筛选到期日且只返回静态合约。三者不能组合成过去时点的原子期权 snapshot，也不能覆盖真实成交、到期和指派事件。因此 HK.00700/US.JD 只能从实时采集启用后向未来积累数据；完整到期周期达标前，报告固定为 `DATA_BLOCKED`，并输出数据质量卡。禁止用期权 OHLC、当前 Greeks 或跨时点数据进行回填。

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
| `-params` | — | 手工/ad-hoc Wheel 参数；可提供完整 `price_position_curve` + `max_inventory`，或两端点 + `max_inventory`；百分比用小数；内部 `hold`/`buy-hold` 不接参数 |
| `-from-watchlist` | false | 按 `-symbol` 从数据库只读加载生产 Wheel 参数和真实 `config_version`；与 `-params`、`-file`、`-symbols` 互斥。生产绑定报告必须使用此开关，手工参数报告的版本显式为 `null` |
| `-fee` | 0 | 每笔实际成交的固定费用；正股与期权 fill 均从现金扣除，未成交/HOLD/机械到期不收费 |
| `-seed` | 42 | 未成交启发式抽样种子；同输入同 seed 产生同一成交 trace，`0` 等价于默认 42 |
| `-max-drawdown` | 0 | 结果约束（0..1）；超限退出 1 |
| `-save` | false | 保存 metrics、完整 `strategy_params`、equity/trades/signals trace；要求 `-dsn` |
| `-report` / `-report-dir` | false / `./reports` | 单标的运行输出 schema 1.1 的 `{report_id}.json` 与确定性 HTML；目录自动创建，同 ID 重跑覆盖 |
| `-cache` | false | 显式把本次 `-report` 证据按 symbol 幂等写入 `strategy_cache`；要求单标的 `-dsn -strategy wheel -from-watchlist -report`，初始状态固定为 `RESEARCH_CANDIDATE` |
| `-train` | 空 | 对 JSON 指定的战术参数范围运行 ES；只支持单标的 `-dsn -strategy wheel`，战略参数仍由 `-params` 固定 |
| `-population` / `-max-generations` | 20 / 40 | ES 种群（16–24）与最大代数 |
| `-budget` / `-train-timeout` | 840 / 10m | 总回测评估预算（含样本外测试）与墙钟超时；启动连接数据源前打印预计评估次数 |
| `-early-stop-patience` | 8 | 验证集连续未达绝对及相对改善阈值后的早停代数 |
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

最大库存必须为正整数。配置可使用满仓价/清仓价两端点，也可使用至少两个价格严格递增、目标库存不递增且不超过最大库存的 `price_position_curve`；多点曲线逐段线性插值并在两端钳制，任何中间锚点都不会被压成端点。百分比字段使用小数（`0.018 = 1.8%`），新战术字段省略时默认为 0 并关闭对应行为。策略无每日次数限制；报告可保留每日提醒/成交次数统计。库存事件至少记录：`stock_shares`、`futures_equivalent_shares`、`option_delta_stock`、`actual_inventory`、`effective_inventory`、`target_inventory`、`inventory_gap`。有效库存为实际库存加带符号期权 Delta；空 Put 增加、空 Call 减少有效库存。

每个事件必须保留原子快照标识、配置版本、候选列表、拒绝原因和动作。动作只有：

- `ALERT`：快照完整、能力状态为 `READY`、候选通过全部硬门槛，供人工评估；
- `HOLD`：数据不完整、状态不允许、库存缺口在无交易区间，或所有候选被拒绝。

候选至少需要 `expiry`、`strike`、`delta`、`bid`、`ask`、`implied_vol`、`theta`、`volume`、`open_interest`、`lot_size`、`observed_at`、`source`。缺任一字段、盘口过期/倒挂、零流动性、DTE 越界、质量分不足或风险约束失败，必须写入拒绝原因并保持 `HOLD`。

## 当前 trace 语义与事件级阻塞

当前每根 bar 的顺序是：读取 bar → 选择截至该 bar 时点的最新原子 snapshot → 运行 Wheel → 在 bar close 机械结算 → 写入 equity/trade/signal trace。snapshot loader 的 `limit` 表示完整批次数，不在 SQL 行级截断多合约批次；若设置开始时间，还会向前读取一个配置 freshness 窗口，使首根 bar 能使用仍新鲜的前置 snapshot。snapshot 不会跨批次拼接，未来时间的 snapshot 不会泄漏到当前 bar；没有所需方向的 `observed_at <= bar.ts` 可信批次时，Wheel signal 为 `DATA_BLOCKED/HOLD`。普通风险限制产生的 HOLD 保持 `capability_status=READY`，两者不可混为一类。这是一种可复现的 bar-time replay，不是事件驱动回测。

事件驱动的到期、指派、人工确认和成交回填指标仍是 `DATA_BLOCKED`：它们需要覆盖目标日期/DTE 的完整历史 snapshot、quote/成交事件顺序和人工审计事实。已保留 snapshot schema、runner trace、机械到期结算和 deterministic fixtures；解锁证据必须包含供应商/历史数据字段映射、原子性/新鲜度测试、端到端可复现 trace 和最大库存违规为零。禁止用 OHLC、固定 Delta、默认流动性、事后中间价或 bar-time replay 冒充事件证据。

## 指标与 trace

事件回测完成后，若数据闸门已启用，至少报告：总收益、最大回撤、指派率、Call 被行权机会成本、订单/提醒频率、库存偏差和最大库存违规数。trace 至少区分：信号、未执行信号、人工确认、人工回填成交、到期和指派；系统不生成 broker order id，也不调用交易 API。

当前确定性运行结果的 `Result.Unfilled` 记录期权卖出成交尝试口径：`AttemptCount = FillCount + UnfilledCount`，`UnfilledRatio = UnfilledCount / AttemptCount`；没有成交尝试时比例为 `null`，CLI 显示“未成交 N/A”，不得解释为 0%。`Trade.Filled=false` 表示一次由 `Trade.UnfilledModel` 标识的模拟未成交卖出尝试，不入账、不改变现金或持仓；成交的期权交易为 `Filled=true`。正股交易、HOLD 与 DATA_BLOCKED 不进入该尝试分母。

`-report` 以 [[BACKTEST_REPORT]] schema 1.1 JSON 为唯一事实源，并用 Go `html/template` 投影同构 HTML。`report_id = bt-{symbol}-{run_seed}-{输入哈希前8位}`；输入不变时 JSON/HTML 字节不变并覆盖原文件。百分比在 JSON 中统一使用小数，时间统一输出 RFC3339 UTC `Z`。Wheel 历史能力为 `DATA_BLOCKED` 时，`net_return_*` 和超额字段为 `null`；只保留明确标为窗口末账面估值变动的 `window_mark_to_market_*`，不得显示成可执行收益。

报告 `terminal` 卡保存现金、正股和期权持仓末值、开放期权腿、带成本基础的已实现/未实现 P&L，以及机械到期/指派统计。开放腿缺 mark 时组合末值/P&L 显式 `null`；真实券商到期/指派计数因历史事件缺失也显式 `null`。`data_quality` 卡保存总/阻塞 bar、有效覆盖率、snapshot 批次/合约行、逐字段缺失计数和完整到期周期闸门。数据库中没有任何 snapshot 时不再只返回错误，而是对已有 bars 生成全程 `DATA_BLOCKED/HOLD` 报告。

参数研究只允许在离线数据上改变 DTE、候选映射、质量门槛、频率和覆盖率（100%、固定覆盖、随机漏 30%/50%，随机种子可复现）。曲线、最大库存、战略状态和资产配置不参与优化。

### ES 战术参数训练

`-train` 的键只允许 `move_interval_pct`、`min_premium_per_share`、`stock_switch_pct`、`trade_gap`、`min_option_quality`、`min_dte`、`max_dte`。范围端点可写 JSON 数字或十进制字符串；`trade_gap` 与 DTE 使用整数离散步长，DTE 内部按“最短 DTE + 非负跨度”解码，始终满足 `min_dte <= max_dte`。例如：

```bash
wbot backtest -dsn "$WBOT_PG_DSN" -symbol HK.00883 -strategy wheel \
  -params '{"full_position_price":48,"zero_position_price":55,"max_inventory":22000}' \
  -train '{"move_interval_pct":["0.005","0.03"],"min_option_quality":["0.5","0.8"]}' \
  -report
```

数据严格按时间切为 train/valid/test（60%/20%/20%，不随机打散）；三个阶段使用用途派生且互不相同的 seed，最终候选再以 5 个封存测试 seed 评估。只有样本外 P10 仍超过 buy-hold 基线的候选才进入报告，否则输出“无可推荐参数”。训练报告固定为 `RESEARCH_ONLY`，不会写 watchlist 或 Wheel 配置。

`-cache` 是与训练解耦的显式动作，单次回测和 `-train` 都可使用。缓存 payload 版本为 `strategy-cache-1.0`，只保存最优参数、收益指标、置信区间、能力状态、报告引用和三道批准闸门；不保存或注入逐代轨迹。首次写入即使数据闸门和样本外门槛通过，也因尚无人工批准而保持 `RESEARCH_CANDIDATE`。只有数据闸门、报告结果的样本外门槛和人工批准全部通过，缓存自身才可标为 `APPROVED_CANDIDATE` 并注入 LLM Snapshot；空缓存、超过 30 天、版本不匹配或状态不合格均跳过。该状态只表示研究候选资格，不会写入 `watchlist` 或 `wheel_configs`，产物始终是 `RESEARCH_ONLY`，不等于配置发布。

## CLI/API 一致性与导出

`wbot backtest -save`、`GET /v1/backtests`、`GET /v1/backtests/{id}` 和 export 共用同一落库 trace。详情包含 `equity_curve`、`trades` 和逐 bar `signals`；运行参数包含完整 `strategy_params`。新 trace 同时保存 `capability_status`、`blocked_by`、`snapshot_key`、`snapshot_observed_at`、实际/有效库存和期权 Delta 库存，UI 与 CSV/JSON 导出均保留这些审计字段。人工动作仍属独立审计表；`-report -from-watchlist` 会把 watchlist 的真实 `config_version` 写入报告身份，手工参数不冒充生产版本。导出时间统一 RFC3339 UTC `Z`。服务端执行超时、数据缺失或阻塞时应返回可执行的 `code/message/action`，不能把阻塞伪装为成功。

## 实现对账

- `internal/wheel`：两价区间、库存、状态、候选校验和 `ALERT/HOLD` 决策；P0-A `READY`。
- `internal/strategy`：唯一注册项 `wheel`；bar-time 适配器只消费当前最新原子 snapshot，缺失/过期时固定产生 `DATA_BLOCKED/HOLD`。
- `internal/wheelstore`、`internal/watchlist` 与 migrations 005/007：版本配置、不可变 snapshot、signal/action 审计表、`READY/DATA_BLOCKED` 数据库约束和 watchlist 版本指针；P0-B/P0-C repository 与 PostgreSQL integration `READY`，真实供应商 adapter 与人工写动作仍受阻塞。
- `internal/backtest` / `internal/backtestexec` 与 `internal/db/migrations/006_backtest_signals.sql`：确定性 bar-time replay、完整策略输入、逐 bar signal 保存/导出路径；不能替代事件级 Wheel 快照回测或实时执行。

关联：[[API]] [[DATA_PIPELINE]] [[WHEEL_STRATEGY]] [[ROADMAP]]
