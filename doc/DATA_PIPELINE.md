# 数据管道（v1 骨架）

行情/历史数据的**拉取 → 校验 → 落地 → 调度 → 可观测**闭环，对应 [[ROADMAP]] v1。

## 命令一览

| 命令 | 作用 |
| --- | --- |
| `wbot ingest mock` | 插入一条 mock 拉取 + 3 条示例 bars（demo 源） |
| `wbot ingest file -file <path>` | 从 JSON 文件拉取 bars（每元素 `{"ts":RFC3339,"open","high","low","close","volume"}`） |
| `wbot ingest url -url <url>` | 从 HTTP(S) URL 拉取同格式 JSON bars |
| `wbot ingest futu` | 从 futu-opend-rs 网关拉 K 线（见 [[FUTU]] §8；`-adjust fwd\|none` 默认 fwd） |
| `wbot ingest futu-option` | 期权链日 K + 正股日 K，缓存优先（见 [[FUTU]] §9、[[DATA_STANDARD]]） |
| `wbot ingest status` | 只读列出最近 `ingestion_runs`（`-limit` 可调） |

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

## 调度方式选择

| 方式 | 适用 |
| --- | --- |
| **`-every <interval>`（应用内循环）** | 常驻进程简单定时拉取；进程内处理 SIGINT 优雅退出；失败容忍内建。 |
| **外部调度（cron / systemd timer）** | 需要精确时刻、错峰、多任务错开时；每次调用单次执行，由外部控制节奏。 |

外部 cron 示例（每 15 分钟整点拉一次，范围仅当天）：

```cron
*/15 * * * * wbot ingest url -url 'https://example.com/bars.json' -symbol '700.HK' -timeframe 1m -from "$(date -u -d '-15 minutes' +%FT%TZ)" -to "$(date -u +%FT%TZ)" >>"$HOME/.cache/wbot-ingest.log" 2>&1
```

> 注：示例中的 `$(...)` 由 cron 的 shell 求值；更稳的写法是包一层脚本（参考 `scripts/verify.sh` 的脚本化风格）。`-every` 与外部 cron 二选一即可，不要叠加。

## 相关实现

- `internal/ingest/`：`Source` 接口（mock/file/http）、`RunIngestion`、`RunEvery`/`RunEveryResilient`、`ValidateBars`、`RecentRuns`
- `internal/db/migrations/`：`001_ingestion_runs.sql`、`002_bars.sql`
- 任务轨迹：`doc/tasks/2026-04-18-wbot-ingest-cli.md` 起，至 `doc/tasks/2026-07-31-ingest-time-range.md` 止

关联：[[ROADMAP]] [[README]]
