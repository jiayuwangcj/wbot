# 回测（v2 骨架）

消费 [[DATA_PIPELINE]] 落地数据的确定性回测运行器：逐根 bar 按 ts 升序推进，策略回调决定交易，结算后输出绩效与约束检查。

## 命令

```bash
wbot backtest \
  -dsn "$WBOT_PG_DSN" -symbol DEMO.US -timeframe 1d \   # 或 -file <bars.json>（与 -dsn 互斥）
  -from 2024-06-01T00:00:00Z -to 2024-07-01T00:00:00Z \  # 仅 -dsn 生效
  -cash 10000 -strategy buy-hold -fee 1 -max-drawdown 0.2
```

| flag | 默认 | 说明 |
| --- | --- | --- |
| `-file` / `-dsn` | — | 输入二选一（互斥）：JSON bars 文件（`ingest bars -json` 格式）或 PostgreSQL 直读（回落 `$WBOT_PG_DSN`） |
| `-symbol` / `-timeframe` | `DEMO.US` / `1d` | `-dsn` 输入的选择条件 |
| `-symbols` | 空 | 逗号分隔的多 symbol 列表（如 `HK.00700,US.AAPL`；2+ 个才走多 symbol 路径，1 个等价于 `-symbol`） |
| `-from` / `-to` | 不限 | RFC3339 时间范围（`-dsn` 输入） |
| `-limit` | 10000 | `-dsn` 输入最大 bars 数 |
| `-cash` | 10000 | 初始资金（>0） |
| `-strategy` | `hold` | `hold` / `buy-hold` / `covered-call` / `cash-secured-put`（模板参数见 `-params`） |
| `-params` | — | 策略参数 JSON（仅模板策略），如 `{"strike_pct_otm":0.05}`；非法参数报错 exit 2 |
| `-fee` | 0 | 每笔正股交易固定费用（买入从现金扣、卖出从所得扣） |
| `-max-drawdown` | 0 | 约束检查（0..1）：结果最大回撤超限 → exit 1；0 = 不检查 |
| `-save` | false | 落库 `backtest_results`（要求 `-dsn` 输入）：metrics + equity_curve/trades 明细（migration 003/004），经 `GET /v1/backtests` 读取（doc/API.md） |
| `-export` | 0 | 导出模式：把已保存运行（`-export <id>`）写到 stdout，不再运行（要求 `-dsn` 输入；与 `-file` 互斥） |
| `-format` | `csv` | 与 `-export` 配合：`csv` 或 `json`；非法值 exit 2 |

输出一行摘要：`final_equity=... total_return=... max_drawdown=... bars=...`（确定性：同输入同输出）。`-save` 时另打印 `saved result id=...`；落库的 equity_curve 为每根 bar 结算后市值、trades 为正股买卖/期权开平仓/行权（ITM 行权、OTM 作废）逐笔明细（同输入同输出，见 `internal/backtest` 确定性单测）。

## 结果导出（draft 2026-08-02）

```bash
wbot backtest -dsn "$WBOT_PG_DSN" -export 7              # csv 到 stdout
wbot backtest -dsn "$WBOT_PG_DSN" -export 7 -format json # json 到 stdout
```

与 `GET /v1/backtests/{id}/export` **同序列化器、同输出**（roundtrip 契约，doc/API.md）：`json` 即详情端点 body、`csv` 为 equity_curve/trades 两段（空行分隔）；两种格式的所有时间统一输出 RFC3339 UTC `Z`，不随 serve/CLI 进程时区变化。缺 id（`-export 0`/负数）或格式非法 → exit 2；id 不存在 → exit 1 + 可读错误。

## 服务端执行（v4 阶段 A 切片 4）

`wbot serve` 的 `POST /v1/backtests`（doc/API.md）与 CLI `-dsn` 路径共用同一运行器（`internal/backtestexec`，draft-2026-08-02-oneclick-backtest）：同一套策略/参数校验（`Build`）、同一查询/运行路径（`Run`）、同一落库 params 形状（`SaveParams`），同输入同输出。单进程互斥（busy → 409）+ 执行超时（默认 5 分钟 → 503）；`from_watchlist` 全量模式逐条串行执行并分别落库。

## 多 symbol 组合（v2 最小语义）

`-symbols A,B,C`（逗号分隔，仅 `-dsn` 输入）把初始资金等分（cash/N）为 N 个独立子账户，各自运行同一策略，输出组合汇总 + 每 symbol 一行子账户汇总：

```bash
wbot backtest -dsn "$WBOT_PG_DSN" -symbols HK.00700,US.AAPL -timeframe 1d -strategy buy-hold
# final_equity=... total_return=... max_drawdown=... bars=... symbols=2
#   HK.00700: final_equity=... total_return=... max_drawdown=... bars=...
#   US.AAPL: final_equity=... total_return=... max_drawdown=... bars=...
```

- **时间对齐**：intersection——各 symbol 按 `[from,to]`（`QueryBars`）取数后，只有每个 symbol 都有的 ts 参与回测（按时刻对齐，跨时区等价 ts 亦对齐）；窗口外/缺失的 bar 不参与。
- **静态等权**：每个子账户初始资金 = cash/N，不自动再平衡（非目标，待产品组确认后续语义）。
- **估值**：每 bar 按各 symbol 自己的 close 估值（复用单 symbol 运行器 `RunOptions`）；组合 equity = 各子账户逐 bar 求和，组合回撤按求和曲线计算。
- **状态隔离**：每个子账户一个全新策略实例（有状态策略如 buy-hold 不可跨账户复用）；`-symbols` 单 symbol 与 `-symbol` 行为完全一致，`Run`/`RunOptions` 签名与单 symbol 行为不变（入口：`backtest.RunMulti`，DB 路径：`backtestexec.RunMulti`）。
- **限制（最小语义）**：多 symbol 不支持 `-file` 输入、期权模板策略（covered-call/cash-secured-put 需 per-symbol option_quotes）、`-save`；`-max-drawdown` 按组合曲线检查。

## 期权腿与策略模板（slice ⑫-b）

- **期权腿**（`internal/backtest`）：`Action` 增 `sell-call / buy-call / sell-put / buy-put`（size = 合约数）；`State.Options` 存开仓腿（`Code/Kind/Strike/Expiry/Lot/Contracts/AvgPremium`，Contracts 负 = 短腿）；腿的**到期结算由 runner 机械执行**：`bar.Ts ≥ Expiry` 时 ITM 按 strike 行权（Call 卖出/买入 `lot×contracts` 股，Put 反向），OTM 作废移除；CSP 开仓强制现金储备校验（`Cash ≥ strike×lot×contracts`）。
- **期权腿数据**：`RunOptions` 注入 `OptionsData{Chain, Bars}`（CLI 从 `option_quotes` 读，映射见 `backtest.OptionsDataFromQuotes`）；腿按「主 symbol 时间轴 + 最新 `close ≤ bar.Ts`」估值，`State.Equity` 纳入市值。`Run` 签名与行为不变（无腿时等价）。
- **策略模板**（`internal/strategy` 注册表）：`Templates()` / `Factory(name, params)`，参数 schema 校验（未知参数/类型/范围报错）。
- **covered-call**：买 `lot_size` 股正股 + 卖 1 张价外看涨，到期结算后滚仓（被行权则先补回正股再卖）。
- **cash-secured-put**：现金担保卖价外看跌，张数 = `cash / (cash_reserve × strike × lot)`（不足 1 张报错）；被行权按 strike 买入，下一 bar 市价卖出后滚仓。

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `strike_pct_otm` | 0.03 | 目标行权价偏离率：call = 现价×(1+pct)，put = 现价×(1-pct)，就近选 chain 档 |
| `expiry_rule` | `next_expiry` | `next_expiry`（最近到期）或 `days`（按 `days_to_expiry` 天选最近档） |
| `days_to_expiry` | 28 | `expiry_rule=days` 的目标到期天数 |
| `fee_per_contract` | 0 | 每张合约费用（从权利金中扣除） |
| `lot_size` | 100 | 合约乘数（`option_quotes` 无 lot 列，以参数为准） |
| `cash_reserve` | 1 | 仅 cash-secured-put：现金担保倍率（≥1） |

```bash
wbot backtest -dsn "$WBOT_PG_DSN" -symbol HK.00700 -adjust none \
  -strategy covered-call -params '{"strike_pct_otm":0.05,"fee_per_contract":5}'
wbot backtest -dsn "$WBOT_PG_DSN" -symbol HK.00700 -adjust none \
  -strategy cash-secured-put -params '{"cash_reserve":1.2}'
```

模板策略必须 `-dsn` 输入（读 `option_quotes`），`-file` 仅支持 `hold`/`buy-hold`。

## 行为保证

- **输入校验**：空 bars、`cash<=0`、`fee<0`、超买/超卖、非法 `-max-drawdown` 值均报错拒绝。
- **结算**：按每根 bar 的 close 价成交；buy/sell/hold 三态；`Strategy` 接口可扩展自定义策略（`internal/backtest/strategy.go`）。
- **指标**：最终 equity、总收益 `(equity-cash)/cash`、最大回撤（equity 曲线峰值到谷值最大跌幅；equity 峰非正时 max_drawdown 为 0）。
- **约束**：`CheckMaxDrawdown`（`internal/backtest/constraint.go`）纯函数判定，CLI 超限 exit 1，便于脚本化门禁（如 CI 里「回撤不得超 X%」）。

## 示例（本地全流程）

```bash
docker compose -f configs/docker-compose.yml up -d
export WBOT_PG_DSN='postgres://postgres:postgres@localhost:5432/wbot_test?sslmode=disable'
wbot ingest mock
wbot backtest -symbol DEMO.US -timeframe 1d -strategy buy-hold
# final_equity=12100 total_return=0.21 max_drawdown=0 bars=3
```

## 实现

- `internal/backtest/`：`state.go`（State/Equity/期权腿）、`strategy.go`（Action/Strategy/Hold/BuyHold）、`backtest.go`（Run/RunOptions/到期结算）、`multi.go`（RunMulti：intersection 对齐 + 等权子账户 + 组合曲线）、`options_data.go`（option_quotes → OptionsData）、`constraint.go`（CheckMaxDrawdown）
- `internal/strategy/`：模板注册表（`strategy.go`）+ covered-call / cash-secured-put（`options.go`）
- 任务轨迹：`doc/tasks/2026-07-31-backtest-runner-slice1.md` → `-dsn-input` → `-fee-placeholder` → `-constraint` → 期权腿 + 策略模板（slice ⑫-b，[[draft-2026-08-01-strategy-options]]）

关联：[[DATA_PIPELINE]] [[API]] [[ROADMAP]]（v2）
