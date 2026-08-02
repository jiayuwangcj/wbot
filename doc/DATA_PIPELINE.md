# 数据管道（v1 骨架）

行情/历史数据的**拉取 → 校验 → 落地 → 调度 → 可观测**闭环，对应 [[ROADMAP]] v1。

## 命令一览

| 命令 | 作用 |
| --- | --- |
| `wbot ingest mock` | 插入一条 mock 拉取 + 3 条示例 bars（demo 源） |
| `wbot ingest file -file <path>` | 从 JSON 文件拉取 bars（每元素 `{"ts":RFC3339,"open","high","low","close","volume"}`） |
| `wbot ingest url -url <url>` | 从 HTTP(S) URL 拉取同格式 JSON bars |
| `wbot ingest futu` | 从 futu-opend-rs 网关拉 K 线（见 [[FUTU]] §8；`-adjust fwd\|none` 默认 fwd） |
| `wbot ingest futu-option` | 期权链日 K + 正股日 K，缓存优先（见 [[FUTU]] §10、[[DATA_STANDARD]]） |
| `wbot ingest account` | 经 OpenD protobuf（只读 funds 查询）把账户资金快照写入 `account_snapshots`（资产曲线数据层；见下文 §账户资产快照、[[FUTU]] §9） |
| `wbot ingest status` | 只读列出最近 `ingestion_runs`（`-limit` 可调） |
| `wbot ingest freshness` | 数据新鲜度检查：各 symbol×timeframe 的 max_ts 年龄与三态状态 + 期权区块（underlying×source）；**任一 stale → exit 1**（可接 cron 门禁） |

通用 flags：`-dsn`（默认 `$WBOT_PG_DSN`）、`-source`（来源标签，写 `ingestion_runs.source`）、`-symbol`、`-timeframe`、`-every`（间隔重复）、`-from`/`-to`（RFC3339 时间范围，零值=不限）。

## 行为保证

- **校验**：落库前 `ValidateBars` 拒绝非法 OHLC/时间序数据（见 `internal/ingest`）。
- **数据标准**：bars/option_quotes 带 `adjust`（none/fwd/back）与 `source`（平台）列，PK 含二者，不同复权/平台数据共存可对比（[[DATA_STANDARD]]）。
- **可重复**：bars 以 `(symbol, timeframe, ts, adjust, source)` 唯一，重复写入 `ON CONFLICT DO NOTHING`；`ingest futu-option` 二次运行命中 DB 缓存直接跳过拉取。
- **失败容忍**：`-every` 模式下单轮失败打日志继续，不终止整个调度；单次模式（无 `-every`）失败即退出非零。
- **事务**：一次拉取 = 一条 `ingestion_runs`（running → succeeded/failed）+ 全部 bars，同一事务。

## 本地开发 PG

```bash
docker compose -f configs/docker-compose.yml up -d   # postgres:16-alpine（与 CI 同配置）
export WBOT_PG_DSN='postgres://postgres:postgres@localhost:5432/wbot_test?sslmode=disable'
```

集成测（`internal/db`、`internal/ingest`）在未设 `WBOT_PG_DSN` 时自动 skip；设了则跑真实迁移与落库。

## Provider 抽象（数据源注册表）

`internal/ingest/provider.go` 提供按名注册的数据源工厂：`ingest.Register(Provider{Name, New})` 注册、`ingest.NewProvider(name, Config)` 构造 `Source`。内建注册 `mock` / `file` / `url` 三个 provider，构造出的 source 与直接实例化 `mockSource`/`FileSource`/`HTTPSource` 行为完全一致（注册表单测含行为等价断言）。

- **CLI**：`wbot ingest mock|file|url` 各支持 `-provider <name>`，默认按子命令推断（mock→mock、file→file、url→url）；未注册的 provider 名 → 报错退出 2。
- **配置承载**：`Config` 为 `map[string]string`，只透传非敏感选项（如 `path`、`url`）；**凭证/token 不放入 Config、不入 `ingestion_runs`**——provider 自行从环境变量（或 `~/.wbot/config.yaml` 渲染出的 env）读取（[[PRIVACY]]）。
- **接新数据源**：实现 `Source` 并 `Register` 一个 provider 即可被 CLI 选用；真实行情源接入为后续 Issue（见 `doc/issues/draft-2026-07-31-ingest-provider-abstraction.md`）。

## 调度方式选择

| 方式 | 适用 |
| --- | --- |
| **`-every <interval>`（应用内循环）** | 常驻进程简单定时拉取；进程内处理 SIGINT 优雅退出；失败容忍内建。 |
| **外部调度（cron / systemd timer）** | 需要精确时刻、错峰、多任务错开时；每次调用单次执行，由外部控制节奏。 |

外部 cron 示例（每 15 分钟整点拉一次，范围仅当天）：

```cron
*/15 * * * * wbot ingest url -url 'https://example.com/bars.json' -symbol 'HK.00700' -timeframe 1m -from "$(date -u -d '-15 minutes' +%FT%TZ)" -to "$(date -u +%FT%TZ)" >>"$HOME/.cache/wbot-ingest.log" 2>&1
```

> 注：示例中的 `$(...)` 由 cron 的 shell 求值；更稳的写法是包一层脚本（参考 `scripts/verify.sh` 的脚本化风格）。`-every` 与外部 cron 二选一即可，不要叠加。

## 账户资产快照（资产曲线）

`wbot ingest account [-env sim|real] [-acc-id <id>] [-addr <proto>] [-dsn] [-every <interval>]` 经 OpenD protobuf（TCP 11111，只读 funds 查询）把账户资金快照写入 `account_snapshots` 表（env/acc_id/total_assets/cash/market_val/frozen_cash/power/captured_at;UNIQUE env+acc_id+captured_at,幂等）。Dashboard 资产曲线与 `GET /v1/account/snapshots` 读这张表。**与 ingestion_runs 隔离**——账户数据非行情历史,两条线不混（[[PRIVACY]]）。

外部 cron 示例（每小时整点快照一次）：

```cron
7 * * * * wbot ingest account >>"$HOME/.cache/wbot-account.log" 2>&1
```

> 或 `wbot ingest account -every 1h`（应用内循环,二选一）。分钟取 `7` 避开整点洪峰,与行情拉取任务错开。

## 数据新鲜度（freshness）

`wbot ingest freshness [-dsn] [-max-age <dur>]` 按 symbol×timeframe×adjust 列出 bars 表各组合的 `max_ts`、年龄（秒）与状态，**并附加期权区块**（按 underlying×source 聚合 option_quotes 的 `max_ts`），作为「数据停更」的观察闭环（与 `ingest status` 互补：status 看拉取任务成败，freshness 看数据是否新鲜）。

- **三态**：`fresh`（max_ts 年龄 ≤ 阈值，等于阈值算 fresh）、`stale`（超过阈值）、`unknown`（无数据）。
- **阈值**：bars 默认按 timeframe 映射——3 × 名义 bar 间隔，下限 10 分钟（`1d` → 3 天、`1m` → 10 分钟、`5m` → 15 分钟、`1w` → 21 天、`1mo` → 90 天）；无法解析的 timeframe 回退 24h。**期权默认 4h**（日内行情数据，`MaxAgeForOptions`）。`-max-age`（如 `-max-age 24h`）对 bars 与期权全局覆盖。
- **退出码**：任一 stale（bars 或期权）→ `1`；全 fresh / 无数据 unknown → `0`；参数错误 → `2`。cron 门禁示例：

```cron
*/10 * * * * wbot ingest freshness >>"$HOME/.cache/wbot-freshness.log" 2>&1 || notify-freshness-stale
```

- 判定实现：`internal/ingest`（`JudgeFreshness`/`MaxAgeForTimeframe`/`MaxAgeForOptions`/`QueryFreshness`/`QueryOptionFreshness`）；`/v1/admin/cluster` 的 `bars_coverage` 每项带 `max_ts_age_seconds`/`fresh`（同阈值规则，向后兼容，见 [[API]]），`options_freshness` 按 underlying×source 聚合同一三态（默认 4h，与 CLI 期权区块一致）。

## 相关实现

- `internal/ingest/`：`Source` 接口（mock/file/http）、`Provider` 注册表（provider.go）、`RunIngestion`、`RunEvery`/`RunEveryResilient`、`ValidateBars`、`RecentRuns`
- `internal/db/migrations/`：`001_ingestion_runs.sql`、`002_bars.sql`、`004_account_snapshots.sql`
- `internal/ingest/account.go`：`QueryAccountSnapshots`（资产曲线查询）
- 任务轨迹：`doc/tasks/2026-04-18-wbot-ingest-cli.md` 起；后续 ingest 闭环：`2026-07-31-ingest-time-range.md`（-from/-to）、`2026-08-02-ingest-mock-rangeflags.md`（mock 范围参数）、`2026-08-02-ingest-refill.md`（bars 补数据）、`2026-08-03-options-ingest-button.md`（期权链拉取）、`2026-08-03-futu-ingest-account-doc.md`（资金快照文档）、`2026-08-03-ci-option-freshness.md`（freshness 验收远程化）

关联：[[ROADMAP]] [[README]]
