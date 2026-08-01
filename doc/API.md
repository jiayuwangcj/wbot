# API 契约（只读数据接口）

由 `wbot serve` 提供（`-listen` 默认 `127.0.0.1:8080`；`-dsn` 或 `$WBOT_PG_DSN`）。数据面接口（`/v1/bars`、`/v1/runs`、`/v1/health`）只读，面向微信小程序/Web 前端；`/v1/strategies`、`/v1/watchlist` 为关注标的与策略绑定数据面（可写：PUT/DELETE watchlist）；`/v1/admin/*` 为后台管理数据面（`/v1/admin/config` 可写，配置值永不返回）。

## Web UI

`wbot serve` 同时提供嵌入式静态 Web UI（go:embed 构建进二进制，离线可用，零外部资源引用；见 [[ROADMAP]] v4）。

| 路径 | 行为 |
| --- | --- |
| `GET /` | 301 → `/ui/`（精确根匹配 `GET /{$}`；行为变化：原为 JSON 404） |
| `GET /ui/` | 数据页 `index.html`（bars/runs 查询骨架） |
| `GET /ui/admin.html` | 管理页占位（status/cluster/config 只读，slice 8-3） |
| `GET /ui/*` | 其余静态资源（`style.css`、`app.js`；不存在 → 404） |

UI 页面不请求、不渲染任何配置值（PRIVACY 红线，见 [[PRIVACY]]）。API 路径（`/v1/*`）不受 `/ui/` 影响；其余未知路径仍为 JSON 404。

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
| `adjust` | 否 | `fwd` | 复权：`fwd`（前复权，数据标准默认）\| `none`（不复权）\| `back`（后复权），见 [[DATA_STANDARD]]；migration 003 之前的存量行 `adjust='none'`，需显式 `adjust=none` 或重新拉取 |
| `from` | 否 | 不限 | RFC3339，闭区间起点 |
| `to` | 否 | 不限 | RFC3339，闭区间终点 |
| `limit` | 否 | 100 | 最大条数（<=0 报 400） |

响应 `200`：

```json
[
  {"ts": "2024-06-01T00:00:00Z", "open": 100, "high": 101, "low": 99.5, "close": 100.5, "volume": 1000}
]
```

## GET /v1/strategies

策略模板清单（名称 + 参数 schema；数据源 ⑫-b 模板注册表，见 [[ROADMAP]]）。只读、无查询参数。CLI 等价物：`wbot backtest -strategy <name>` 的模板名（模板落地前，本端点按草稿契约硬编码，见 `internal/watchlist`）。

响应 `200`：

```json
[
  {
    "name": "covered-call",
    "description": "备兑看涨：持有正股 + 卖出看涨",
    "params": [
      {"name": "strike_pct_otm", "type": "number", "default": 0.03, "description": "行权价偏离度：行权价 = 现价×(1+pct) 就近 chain 档"},
      {"name": "expiry_rule", "type": "choice", "default": "next_expiry", "choices": ["next_expiry"], "description": "到期选择规则"},
      {"name": "days_to_expiry", "type": "number", "default": 28, "description": "目标到期天数"},
      {"name": "fee_per_contract", "type": "number", "default": 0, "description": "每合约费用"}
    ]
  },
  {"name": "cash-secured-put", "description": "现金担保看跌：卖出看跌、现金担保", "params": [同上]}
]
```

| 字段 | 说明 |
| --- | --- |
| `name` | 模板名（PUT watchlist 的 `strategy` 取值） |
| `params[].name` | 参数名 |
| `params[].type` | `number` \| `string` \| `choice`（PUT 时按此校验，非法 400） |
| `params[].default` | 缺省值（缺省时不传该参数即可） |
| `params[].choices` | `choice` 类型的合法取值 |

## GET /v1/watchlist

关注标的列表（`watchlist` 表，migration 003），按 `symbol` 升序。只读、无查询参数。

响应 `200`：

```json
[
  {"symbol": "HK.00700", "strategy": "covered-call",
   "params": {"strike_pct_otm": 0.03},
   "created_at": "2026-08-01T08:00:00Z", "updated_at": "2026-08-01T08:00:00Z"}
]
```

| 字段 | 说明 |
| --- | --- |
| `symbol` | 关注标的（PK，如 `HK.00700`） |
| `strategy` | 绑定策略模板名 |
| `params` | 该标的的独立参数（JSONB 原样返回；未传时为 `{}`） |
| `created_at` / `updated_at` | RFC3339；PUT 更新时 `created_at` 保留、`updated_at` 刷新 |

## PUT /v1/watchlist/{symbol}

添加或更新（upsert，`ON CONFLICT (symbol) DO UPDATE`）一个标的的策略绑定。body `{"strategy": "...", "params": {...}}`；`strategy` 必填且必须为 /v1/strategies 模板名，`params` 按模板 schema 校验（未知参数、类型不符、choice 越界 → 400），省略 `params` 视为 `{}`（其余参数用模板默认值）。

请求：

```bash
curl -X PUT 'http://127.0.0.1:8080/v1/watchlist/HK.00700' \
  -H 'Content-Type: application/json' \
  -d '{"strategy":"covered-call","params":{"strike_pct_otm":0.03}}'
```

响应 `200`（存储后的条目，形状同 GET /v1/watchlist 元素）。

| 状态码 | 场景 |
| --- | --- |
| 200 | 添加/更新成功 |
| 400 | 缺 `strategy` / 未知模板 / 非法参数 / body 非 JSON / symbol 为空 |
| 405 | 方法不允许（非 PUT/DELETE） |

## DELETE /v1/watchlist/{symbol}

移除一个标的。响应 `200`：`{"symbol": "HK.00700", "deleted": true}`；标的不在列表时 `404`（`{"error": "not found"}`）。

CLI 等价物：`wbot watchlist add|remove|list`（`-symbol -strategy -params '<json>'`；列表输出 `symbol strategy params` 一行一条）。

PRIVACY：本端点不涉及配置值（watchlist 参数为用户业务数据，非凭证；API 永不返回配置值，见 doc/PRIVACY.md）。

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
| `listen_addr` | 实际绑定地址（`ln.Addr()`；`-listen 127.0.0.1:0` 时报告实际端口） |
| `db.ok` | DB Ping 结果（≤3s 超时） |
| `db.latency_ms` | ping 耗时（毫秒；DB 不可用时省略） |

DB 不可用时仍返回 `200`，`db.ok` 为 `false`（信息端点；健康语义归 /v1/health 的 503）。

PRIVACY：本端点无配置值字段（API 永不返回配置值，见 doc/PRIVACY.md）。

## GET /v1/admin/cluster

集群状态组件视图。**单进程语义**：wbot 为单进程 CLI，无真实集群；端点命名 `cluster` 仅为对齐需求方原话，`components` 是本进程四个组件的状态聚合（进程 / DB / 数据管道 / 数据面），不扩展 master/agent 注册。无查询参数。
## GET /v1/admin/config

配置 key 清单与设置状态（后台管理数据面；无查询参数）。**永不返回配置值**——PRIVACY 红线（见 doc/PRIVACY.md）：值只进 `~/.wbot/wbot.conf`（0600、tmp+rename 原子写），本端点只回 key 元数据。

响应 `200`（key 按白名单顺序）：

```json
[
  {"key": "credentials.wechat.appid", "group": "credentials.wechat", "set": false, "updated_at": null},
  {"key": "system.listen", "group": "system", "set": true, "updated_at": "2026-07-31T08:00:00Z"}
]
```

| 字段 | 说明 |
| --- | --- |
| `key` | 白名单 key（点分命名，共 9 个） |
| `group` | 分组（如 `credentials.wechat`） |
| `set` | 是否已写入 `~/.wbot/wbot.conf` |
| `updated_at` | 最近写入时间（RFC3339；未设置时为 `null`） |

key 白名单：`credentials.wechat.{appid,secret,token}`、`credentials.schwab.{api_key,account}`、`credentials.ibkr.{gateway_host,gateway_port,account}`、`system.{listen}`。

## PUT /v1/admin/config/{key}

写入/覆盖单个配置值；body `{"value": "..."}`。校验：key 必须为白名单内（否则 404）、值非空且 ≤4096 字符（否则 400）。持久化到 `~/.wbot/wbot.conf`（0600、tmp+rename 原子写）。**响应不含值**（PRIVACY 红线）。

响应 `200`：

```json
{
  "components": {
    "process": {
      "version": "0.0.0-dev",
      "pid": 12345,
      "started_at": "2026-07-31T08:00:00Z",
      "uptime_seconds": 12.5,
      "listen_addr": "127.0.0.1:8080"
    },
    "db": {"ok": true, "latency_ms": 1.2},
    "pipeline": {
      "counts": {"running": 1, "succeeded": 9, "failed": 0},
      "recent_runs": [
        {"id": 10, "source": "cli-mock", "status": "succeeded",
         "started_at": "2026-07-31T08:00:00Z", "finished_at": "2026-07-31T08:00:01Z"}
      ]
    },
    "data_plane": {
      "bars_coverage": [
        {"symbol": "DEMO.US", "timeframe": "1d", "count": 3,
         "min_ts": "2024-06-01T00:00:00Z", "max_ts": "2024-06-03T00:00:00Z"}
      ]
    }
  }
}
```

| 组件 | 字段 | 说明 |
| --- | --- | --- |
| `process` | version / pid / started_at / uptime_seconds / listen_addr | 同 /v1/admin/status 进程字段 |
| `db` | ok / latency_ms | Ping 结果；down → `ok:false`，仍返回 200（同 /v1/admin/status 语义） |
| `pipeline.counts` | running / succeeded / failed | `ingestion_runs` 全表按状态计数 |
| `pipeline.recent_runs` | 最近 5 条 | 形状同 /v1/runs；`finished_at` 为 `null` 表示仍在运行 |
| `data_plane.bars_coverage` | symbol / timeframe / count / min_ts / max_ts | `bars` 表各 symbol×timeframe 组合的条数与 ts 区间 |

DB 不可用时（ping 失败）：仍返回 `200`，`db.ok` 为 `false`，且**不执行** pipeline/data_plane 查询——`counts` 全 0、`recent_runs` 与 `bars_coverage` 为空数组（降级语义同 /v1/admin/status；进程字段照常返回）。

ping 通过但存储查询失败时返回 `500`（`{"error": "internal error"}`）。

PRIVACY：本端点无配置值字段（API 永不返回配置值，见 doc/PRIVACY.md）。
{"key": "credentials.wechat.appid", "set": true}
```

| 状态码 | 场景 |
| --- | --- |
| 200 | 写入成功 |
| 400 | 空值 / 值超长 / body 非 JSON |
| 404 | 白名单外 key |
| 405 | 方法不允许（非 GET/PUT） |

PRIVACY：API 永不返回配置值——GET 只回 key 元数据、PUT 响应不含 value；值仅存于 `~/.wbot/wbot.conf`（见 doc/PRIVACY.md）。

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
| 缺必填参数 / 参数非法（坏时间、limit<=0、空/超长配置值、body 非 JSON、未知策略模板或非法 watchlist 参数） | 400 |
| 存储查询失败 | 500 |
| DB ping 失败 | 503 |
| 未知路径 / 白名单外 config key / DELETE 不存在的 watchlist 标的 | 404 |
| 方法不允许（非 GET/PUT/DELETE；watchlist 标的路径仅支持 PUT/DELETE） | 405 |

## 本地验证

```bash
docker compose -f configs/docker-compose.yml up -d
export WBOT_PG_DSN='postgres://postgres:postgres@localhost:5432/wbot_test?sslmode=disable'
wbot ingest mock
wbot serve &
curl -s 'http://127.0.0.1:8080/v1/runs'
curl -s 'http://127.0.0.1:8080/v1/bars?symbol=DEMO.US&timeframe=1d'
curl -s 'http://127.0.0.1:8080/v1/admin/cluster'
curl -s 'http://127.0.0.1:8080/v1/strategies'
curl -X PUT 'http://127.0.0.1:8080/v1/watchlist/HK.00700' -H 'Content-Type: application/json' \
  -d '{"strategy":"covered-call","params":{"strike_pct_otm":0.03}}'
curl -s 'http://127.0.0.1:8080/v1/watchlist'
curl -X DELETE 'http://127.0.0.1:8080/v1/watchlist/HK.00700'
```

关联：[[DATA_PIPELINE]] [[ROADMAP]]（v4 Go API，微信小程序前置依赖）
