# 数据标准（DATA_STANDARD）

统一行情/回测/关注标的的落库口径，保证不同平台数据可对比、可合并。关联 [[DATA_PIPELINE]]、[[FUTU]]。

## 复权（adjust）

所有行情表（`bars`、`option_quotes`）带 `adjust text NOT NULL DEFAULT 'none'`：

| adjust | 含义 | 富途 rehab_type（[[FUTU]] §8 实测） |
| --- | --- | --- |
| `none` | 不复权（原始价） | 0 |
| `fwd` | 前复权（回测推荐） | 1 |
| `back` | 后复权 | 2 |

- **PK 含 adjust**：同一 symbol/timeframe/ts 不同复权是不同数据，各自成行
- 拉取指定复权：`wbot ingest futu -adjust fwd|none`（默认 `fwd`，回测用）；migration 003 之前的存量行 `adjust='none'`（列默认值）

## 来源（source）

`source text NOT NULL DEFAULT 'futu'`（数据平台标识：futu / 未来其他平台统一该字段）。PK 含 source：同一 symbol+timeframe+ts+adjust 的多来源数据可共存，按 source 区分做一致性校验。

## 时间基准

- `ts timestamptz` 落库一律 **UTC**：网关返回 `+08` 墙钟 + epoch 秒，落库取 epoch 对应的 UTC 瞬时（`time.Unix(...).UTC()`）
- **输出面**（2026-08-03 实测）：CLI 打印与 HTTP JSON 按进程本地时区渲染 RFC3339 偏移——本机 `+08`，如 `2026-08-03T05:14:30+08:00` = UTC `2026-08-02T21:14:30Z`（同一瞬时）。落库值不变，消费方按 RFC3339 解析即可；[[API]] 示例 `Z` 字面仅示意。
- `expiry`/`strike_time` 为期权到期日（date，无时区）
- 时间范围查询用 RFC3339 UTC（`-from`/`-to`）

## 单位与格式

- 价格 `double precision`：原始货币单位（HKD/USD/CNY），复权按 adjust 语义
- `volume bigint`：股/份数；未成交合约 volume=0 属正常
- symbol 格式 `MARKET.CODE`：正股 `HK.00700`（market 枚举 1=HK 11=US 21=SH 22=SZ）；期权 `HK.TCH260807C335000`（前缀+到期+Call/Put+行权价×1000）

## 表结构（migration 003）

| 表 | 用途 | 关键列 / PK |
| --- | --- | --- |
| `bars` | 正股/指数 OHLCV | `(symbol, timeframe, ts, adjust, source)` |
| `option_quotes` | 期权合约日 K | `(symbol, ts, adjust, source)`；`underlying/strike/expiry/option_type` 冗余 |
| `backtest_results` | 回测结果 | `id`；`params`/`metrics` jsonb |
| `watchlist` | 关注标的 | `symbol` PK |

## 缓存语义（不能每次都拉）

- `wbot ingest futu-option`：先查 DB（`option_quotes` 按 underlying+adjust+时间窗、`bars` 按 symbol+timeframe+adjust）——窗口内有数据即 **cache hit 跳过拉取**（打印行数），否则拉取落库
- `wbot ingest futu`：`ON CONFLICT DO NOTHING` 幂等（同键重拉不覆盖）
- 一致性校验：同 symbol+timeframe+adjust 的不同 source 行可查询对比（各 provider 独立 source 标签：CLI mock/file/url 默认 `cli-mock`/`cli-file`/`cli-url`（`-source` 可覆盖），futu 系写入平台源；dev-up 种子即 mock 写入，与 futu 数据同键共存可对比）
