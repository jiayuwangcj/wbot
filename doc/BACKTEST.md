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
| `-from` / `-to` | 不限 | RFC3339 时间范围（`-dsn` 输入） |
| `-limit` | 10000 | `-dsn` 输入最大 bars 数 |
| `-cash` | 10000 | 初始资金（>0） |
| `-strategy` | `hold` | `hold`（不交易）或 `buy-hold`（首根 bar 全仓买入后持有） |
| `-fee` | 0 | 每笔交易固定费用（占位；买入从现金扣、卖出从所得扣） |
| `-max-drawdown` | 0 | 约束检查（0..1）：结果最大回撤超限 → exit 1；0 = 不检查 |

输出一行摘要：`final_equity=... total_return=... max_drawdown=... bars=...`（确定性：同输入同输出）。

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

- `internal/backtest/`：`state.go`（State/Equity）、`strategy.go`（Action/Strategy/Hold/BuyHold）、`backtest.go`（Run/ParseBars/Result）、`constraint.go`（CheckMaxDrawdown）
- 任务轨迹：`doc/tasks/2026-07-31-backtest-runner-slice1.md` → `-dsn-input` → `-fee-placeholder` → `-constraint`

关联：[[DATA_PIPELINE]] [[API]] [[ROADMAP]]（v2）
