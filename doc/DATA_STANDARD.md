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
- 腾讯接口参数 `qfq` 表示前复权，落库映射为 canonical `adjust='fwd'`；报告保留 provider 语义 `adjusted='qfq'`，避免把腾讯参数名误写成 Futu rehab 名。

## 来源（source）

`source text NOT NULL DEFAULT 'futu'`（数据平台标识，如 `futu` / `tencent` / `hkex`）。PK 含 source：同一 symbol+timeframe+ts+adjust 的多来源数据可共存，按 source 区分做一致性校验。正股 bars 对同一 ts 固定择一：`futu` 优先、`tencent` 补缺、其他 source 按字典序；期权 snapshot 先取不晚于 bar 的最新 `observed_at`，同一时点固定 `futu` → `hkex` → 其他 source 字典序，再按 `snapshot_key`。被实际消费的来源/复权进入报告数据质量卡。

## 时间基准

- `ts timestamptz` 落库一律 **UTC**：网关返回 `+08` 墙钟 + epoch 秒，落库取 epoch 对应的 UTC 瞬时（`time.Unix(...).UTC()`）
- **输出面**（2026-08-03 实测）：CLI 打印与 HTTP JSON 按进程本地时区渲染 RFC3339 偏移——本机 `+08`，如 `2026-08-03T05:14:30+08:00` = UTC `2026-08-02T21:14:30Z`（同一瞬时）。落库值不变，消费方按 RFC3339 解析即可；[[API]] 示例 `Z` 字面仅示意。
- `expiry`/`strike_time` 为期权到期日（date，无时区）
- 时间范围查询用 RFC3339 UTC（`-from`/`-to`）

## 单位与格式

- 价格 `double precision`：原始货币单位（HKD/USD/CNY），复权按 adjust 语义
- `volume bigint`：正股为股/份数；HKEX DTOP 期权为成交合约张数；未成交合约 volume=0 属正常
- symbol 格式 `MARKET.CODE`：正股 `HK.00700`（market 枚举 1=HK 11=US 21=SH 22=SZ）；期权 `HK.TCH260807C335000`（前缀+到期+Call/Put+行权价×1000）

## 表结构（migration 003/005 等）

| 表 | 用途 | 关键列 / PK |
| --- | --- | --- |
| `bars` | 正股/指数 OHLCV | `(symbol, timeframe, ts, adjust, source)` |
| `option_quotes` | 期权合约日 K | `(symbol, ts, adjust, source)`；`underlying/strike/expiry/option_type` 冗余 |
| `option_quote_snapshots`（migration 005） | 原子期权报价批次；含 HKEX 研究态日终投影 | `(underlying, observed_at, snapshot_key, symbol)` |
| `backtest_results` | 回测结果 | `id`；`params`/`metrics` jsonb |
| `watchlist` | 关注标的 | `symbol` PK |

## 缓存语义（不能每次都拉）

- `wbot ingest futu-option`：先查 DB（`option_quotes` 按 underlying+adjust+时间窗、`bars` 按 symbol+timeframe+adjust）——窗口内有数据即 **cache hit 跳过拉取**（打印行数），否则拉取落库
- `wbot ingest futu`：`ON CONFLICT DO NOTHING` 幂等（同键重拉不覆盖）
- `wbot ingest tencent`：固定 `source=tencent,adjust=fwd(qfq)`，同一 symbol/date 重跑 `ON CONFLICT DO NOTHING`；默认剔除北京时间今日的末行，避免形成 K 被幂等写入冻结，次日运行补入完整值（`-include-forming` 显式保留旧行为）；HK.00700 可一次回填 1000+ 日，美股当前仅一日并依靠每日运行向未来积累。
- `wbot ingest hkex`：按交易日下载官方 DTOP + RP006；`option_quotes` 固定 `source=hkex,adjust=none`，并以结算价同时映射 OHLC。研究投影以 `snapshot_key=hkex-eod-YYYYMMDD-bs-r0` 写入 `option_quote_snapshots`；snapshot 冲突不写，quote 冲突只允许在 RP006 后续恢复时补齐原先为 null 的 IV，不覆盖既有官方值。每个交易日单独事务提交，中途失败后可从原区间安全重跑。DTOP 的 `no_trading_activities.txt`/零明细按休市日跳过；若 DTOP 有效但 RP006 明示 `No File Available Yet` 或返回 404，当天仍保留官方 settlement 行并计入 `quote_only_days`，不伪造 IV/Greeks snapshot。
- 一致性校验：同 symbol+timeframe+adjust 的不同 source 行可查询对比（各 provider 独立 source 标签：CLI mock/file/url 默认 `cli-mock`/`cli-file`/`cli-url`（`-source` 可覆盖），futu 系写入平台源；dev-up 种子即 mock 写入，与 futu 数据同键共存可对比）

HKEX DTOP 提供结算价、成交张数和 gross OI，RP006-FINAL 提供系列结算价、IV 百分比及标的结算价。官方值写入 `option_quotes`；为复用现有 bar-time Wheel 研究器，另生成 `bid=ask=结算价`、官方 IV、DTOP 成交/OI、CLI 明示 lot size，以及按 Black-Scholes `r=0` 派生的 Delta/Theta。该投影不包含真实历史买卖盘、利率/股息假设或成交事件，能力只能是 `RESEARCH_ONLY`，不得用于实时 `ALERT`。
