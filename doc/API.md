# API 契约（只读数据接口）

由 `wbot serve` 提供（`-listen` 默认 `127.0.0.1:8080`；`-dsn` 或 `$WBOT_PG_DSN`）。当前只读，面向微信小程序/Web 前端对齐。

## GET /v1/runs

最近 ingestion runs（`ingestion_runs` 表）。

Query 参数：

| 参数 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `limit` | 否 | 10 | 返回条数（<=0 报 400） |

响应 `200`：

```json
[
  {"id": 3, "source": "cli-mock", "status": "succeeded",
   "started_at": "2026-07-31T08:00:00Z", "finished_at": "2026-07-31T08:00:01Z"},
  {"id": 2, "source": "cli-file", "status": "succeeded",
   "started_at": "2026-07-31T07:00:00Z", "finished_at": null}
]
```

`finished_at` 为 `null` 表示仍在运行。

## GET /v1/bars

已落库 OHLCV bars（`bars` 表），按 `(ts)` 升序。

Query 参数：

| 参数 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `symbol` | 是 | — | 如 `DEMO.US` |
| `timeframe` | 是 | — | 如 `1d`、`1m` |
| `from` | 否 | 不限 | RFC3339，闭区间起点 |
| `to` | 否 | 不限 | RFC3339，闭区间终点 |
| `limit` | 否 | 100 | 最大条数（<=0 报 400） |

响应 `200`：

```json
[
  {"ts": "2024-06-01T00:00:00Z", "open": 100, "high": 101, "low": 99.5, "close": 100.5, "volume": 1000}
]
```

<<<<<<< HEAD
## GET /v1/admin/status

进程 + DB 运行状态（后台管理数据面；无查询参数）。

响应 `200`：

```json
{
  "version": "0.0.0-dev",
  "pid": 12345,
  "started_at": "2026-07-31T08:00:00Z",
  "uptime_seconds": 12.5,
  "listen_addr": "127.0.0.1:8080",
  "db": {"ok": true, "latency_ms": 1.2}
}
```

| 字段 | 说明 |
| --- | --- |
| `version` | 构建时 `-ldflags` 注入的版本号 |
| `pid` | 进程 PID |
| `started_at` | serve 进程启动时间（RFC3339） |
| `uptime_seconds` | 自启动以来的秒数 |
| `listen_addr` | `serve -listen` 启动参数 |
| `db.ok` | DB Ping 结果（≤3s 超时） |
| `db.latency_ms` | ping 耗时（毫秒；DB 不可用时省略） |

DB 不可用时仍返回 `200`，`db.ok` 为 `false`（信息端点；健康语义归 /v1/health 的 503）。

PRIVACY：本端点无配置值字段（API 永不返回配置值，见 doc/PRIVACY.md）。
## GET /v1/health

健康检查（微信小程序前置探测）：对数据库执行 ping（≤3s 超时），只读、无参数。

响应 `200`（数据库可达）：

```json
{"status": "ok"}
```

响应 `503`（数据库不可达）：

```json
{"error": "database unavailable"}
```

## 错误

统一 `{"error": "..."}` JSON：

| 场景 | 状态码 |
| --- | --- |
| 缺必填参数 / 参数非法（坏时间、limit<=0） | 400 |
| 存储查询失败 | 500 |
| DB ping 失败 | 503 |
| 未知路径 | 404 |
| 非 GET | 405 |

## 本地验证

```bash
docker compose -f configs/docker-compose.yml up -d
export WBOT_PG_DSN='postgres://postgres:postgres@localhost:5432/wbot_test?sslmode=disable'
wbot ingest mock
wbot serve &
curl -s 'http://127.0.0.1:8080/v1/runs'
curl -s 'http://127.0.0.1:8080/v1/bars?symbol=DEMO.US&timeframe=1d'
```

关联：[[DATA_PIPELINE]] [[ROADMAP]]（v4 Go API，微信小程序前置依赖）
