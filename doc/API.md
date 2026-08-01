# API 契约（只读数据接口）

由 `wbot serve` 提供（`-listen` 默认 `127.0.0.1:8080`；`-dsn` 或 `$WBOT_PG_DSN`）。数据面接口（`/v1/bars`、`/v1/runs`、`/v1/health`）只读，面向微信小程序/Web 前端；`/v1/strategies`、`/v1/watchlist` 为关注标的与策略绑定数据面（可写：PUT/DELETE watchlist）；`/v1/backtests` 为回测执行与结果数据面（GET 读取，含 `/{id}/export` csv/json 下载；写入方为 CLI `wbot backtest -save` 与 POST /v1/backtests，同一运行器路径，见 [[BACKTEST]]）；`/v1/futu/quote` 为实时行情代理、`/v1/futu/account` 为资金/持仓只读代理、`/v1/futu/options` 为期权链代理（serve 代浏览器访问富途网关，见 [[FUTU]]）；`/v1/admin/*` 为后台管理数据面（`/v1/admin/config` 可写，配置值永不返回）。

## Web UI

`wbot serve` 同时提供嵌入式静态 Web UI（go:embed 构建进二进制，离线可用，零外部资源引用；见 [[ROADMAP]] v4）。

| 路径 | 行为 |
| --- | --- |
| `GET /` | 301 → `/ui/`（精确根匹配 `GET /{$}`；行为变化：原为 JSON 404） |
| `GET /ui/` | 数据页 `index.html`（bars/runs 查询骨架 + 实时报价卡 + 账户卡/持仓表；bars 查询结果显示覆盖范围，来自 `/v1/admin/cluster` 的 `bars_coverage` 或查询结果首末 ts；bars 表单提交同时刷新报价卡，复用其 symbol 输入，走 `/v1/futu/quote`；账户卡/持仓表加载时与「Refresh」按钮走 `/v1/futu/account`） |
| `GET /ui/watchlist.html` | 关注标的页（watchlist CRUD + 策略参数表单，slice 12-c） |
| `GET /ui/results.html` | 回测结果页：列表（可勾选 2 条对比）/ 详情 / 对比视图（指标并排 + equity 曲线叠加，S5） |
| `GET /ui/admin.html` | 管理页（status/cluster/config 只读，slice 8-3） |
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

移除一个标的。响应 `200`：`{"symbol": "HK.00700", "deleted": true}`；标的不在列表时 `404`（错误体见 [[#错误]]，如 `{"code":"not_found","message":"not found","action":"check the path and retry","error":"not found"}`）。

CLI 等价物：`wbot watchlist add|remove|list`（`-symbol -strategy -params '<json>'`；列表输出 `symbol strategy params` 一行一条）。

PRIVACY：本端点不涉及配置值（watchlist 参数为用户业务数据，非凭证；API 永不返回配置值，见 doc/PRIVACY.md）。

## GET /v1/backtests

已保存回测运行列表（`backtest_results` 表，migration 003/004），按 `id` 倒序（最新在前）。只读；写入方为 CLI `wbot backtest -save`（`doc/BACKTEST.md`）。列表为摘要形态：完整 metrics，**不含** equity_curve/trades。

Query 参数：

| 参数 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `symbol` | 否 | 不限 | 按标的过滤（如 `DEMO.US`；缺省返回全部） |
| `strategy` | 否 | 不限 | 按策略名过滤（如 `buy-hold`） |
| `limit` | 否 | 50 | 最大条数（<=0 或非数字报 400） |

响应 `200`：

```json
[
  {"id": 7, "strategy": "buy-hold", "symbol": "DEMO.US",
   "params": {"cash": 10000, "fee": 0, "timeframe": "1d", "adjust": "none"},
   "metrics": {"equity": 10500, "total_return": 0.05, "max_drawdown": 0.02, "bars": 2},
   "start_ts": "2026-07-27T00:00:00Z", "end_ts": "2026-07-28T00:00:00Z",
   "created_at": "2026-08-01T08:00:00Z"}
]
```

| 字段 | 说明 |
| --- | --- |
| `id` | 运行 id（详情端点路径参数） |
| `params` | 运行参数（cash/fee/timeframe/adjust 等，JSONB 原样返回） |
| `metrics` | 摘要指标（equity/total_return/max_drawdown/bars） |
| `start_ts` / `end_ts` | 回测 bars 时间范围（RFC3339） |
| `created_at` | 落库时间（RFC3339） |

无结果时返回 `[]`。

## GET /v1/backtests/{id}

单个运行详情：列表字段 + 完整 `equity_curve`/`trades`（migration 004 落库的确定性 trace；`{id}` 为正整数）。

响应 `200`：

```json
{
  "id": 7, "strategy": "buy-hold", "symbol": "DEMO.US",
  "params": {"cash": 10000, "fee": 0},
  "metrics": {"equity": 10500, "total_return": 0.05, "max_drawdown": 0.02, "bars": 2},
  "start_ts": "2026-07-27T00:00:00Z", "end_ts": "2026-07-28T00:00:00Z",
  "created_at": "2026-08-01T08:00:00Z",
  "equity_curve": [
    {"ts": "2026-07-27T00:00:00Z", "equity": 10000},
    {"ts": "2026-07-28T00:00:00Z", "equity": 10500}
  ],
  "trades": [
    {"ts": "2026-07-27T00:00:00Z", "action": "buy", "symbol": "DEMO.US", "size": 100, "price": 100, "cash_after": 0}
  ]
}
```

| 字段 | 说明 |
| --- | --- |
| `equity_curve[]` | 每根 bar 一根曲线点：`ts`（bar 时间，RFC3339）+ `equity`（结算后组合市值） |
| `trades[]` | 逐笔明细：`action`（`buy`/`sell`/`sell-call`/`buy-call`/`sell-put`/`buy-put`/`exercise-call`/`exercise-put`/`expire-otm`）、`symbol`（期权腿为合约代码，正股为标的）、`size`（正股/行权为股数，期权为合约数）、`price`（正股为成交价、期权为每张权利金、行权为 strike）、`cash_after`（结算后现金） |

migration 004 之前的老行（无曲线）返回 `equity_curve: []`、`trades: []`（不报错）。

| 状态码 | 场景 |
| --- | --- |
| 200 | 找到该运行 |
| 400 | `{id}` 非正整数（如 `abc`、`0`、`-1`） |
| 404 | 该 id 不存在（`backtest_results` 无此行；`action` 提示先 `wbot backtest -save`） |
| 405 | 方法不允许（仅 GET） |

错误体为本切片起的新约定 `{"code", "message", "action", "error"}`（S5 全量接入；`action` 为可执行的补救建议，`error` 为兼容别名，值同 `message`）：

```json
{"code": "not_found", "message": "backtest result 42 not found", "action": "run `wbot backtest -save` to persist a run first", "error": "backtest result 42 not found"}
```

PRIVACY：本端点无配置值字段（API 永不返回配置值，见 doc/PRIVACY.md）。

## GET /v1/backtests/{id}/export

单个运行结果下载（draft 2026-08-02：策略结果可视化的数据出口——外部工具/报告存档）。数据与 `GET /v1/backtests/{id}` 详情**同源同序列化器**（`internal/backtest` Export）：`format=json` 与详情**逐字节一致**（roundtrip 契约），`format=csv` 为同一批数据的两个 section。

Query 参数：

| 参数 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `format` | 否 | `csv` | `csv` 或 `json`；其他值 400（`invalid_request`） |

响应头：`Content-Type: text/csv; charset=utf-8`（csv）/ `application/json`（json）+ `Content-Disposition: attachment; filename="backtest-{id}-{strategy}-{created日期}.{ext}"`。

CSV 结构（单响应两段，空行分隔；每段首行为 section 名、次行为表头）：

```
equity_curve
ts,equity
2026-07-27T00:00:00Z,10000
2026-07-28T00:00:00Z,10500

trades
ts,action,symbol,size,price,cash_after
2026-07-27T00:00:00Z,buy,DEMO.US,100,100,0
```

`equity_curve` 行数 = 详情 `equity_curve` 数组长度、`trades` 同理；migration 004 之前的老行（无曲线）返回 200 + 空 section（仅表头行，兼容语义同详情端点的 `[]`）。

| 状态码 | 场景 |
| --- | --- |
| 200 | 找到该运行（csv 默认；json 与详情一致） |
| 400 | `{id}` 非正整数、`format` 不是 `csv`/`json` |
| 404 | 该 id 不存在（错误体同详情端点） |
| 405 | 方法不允许（仅 GET） |

CLI 等价物：`wbot backtest -dsn "$WBOT_PG_DSN" -export <id> -format csv|json`（stdout 输出，同 API 逐字节一致；见 [[BACKTEST]]）。

PRIVACY：本端点仅回测数据，无配置值/凭证字段（API 永不返回配置值，见 doc/PRIVACY.md）。

## POST /v1/backtests

执行并落库一次回测（v4 阶段 A 切片 4，draft-2026-08-02-oneclick-backtest）。**同步执行**（请求内完成，单回测秒级）；复用 CLI `wbot backtest -dsn` 同一运行器路径（`internal/backtestexec`：同输入同输出——与 CLI `-save` 落库的 metrics/params/equity_curve/trades 一致，见 [[BACKTEST]]）。

**单进程语义**：同一时刻只跑一个回测（互斥锁覆盖整批），busy → 409；执行超时（默认 5 分钟，覆盖整批）→ 503。客户端断开会中止运行且不落库。

请求 body 两种形态（互斥，同传 422）：

1. **手填**：`{"symbol": "HK.00700", "strategy": "covered-call", "params": {"strike_pct_otm": 0.05}}`
   - `symbol` / `strategy` 必填；`strategy` 取值同 CLI `-strategy`（`hold` / `buy-hold` / `covered-call` / `cash-secured-put`），`params` 按模板 schema 校验（未知参数、类型不符、越界 → 422），省略视为 `{}`（模板默认值）；`hold`/`buy-hold` 不得携带 `params`
   - 运行参数取文档化默认值（同 CLI 缺省）：timeframe `1d`、adjust `fwd`、cash 10000、fee 0、limit 10000；本端点不暴露这些输入
2. **全量**：`{"from_watchlist": true}` — 逐条 watchlist（按 symbol 升序）串行执行并分别落库；任一标的失败即中止并返回该错误（此前已落库的运行保留）；watchlist 为空 → 422 `empty_watchlist`

响应 `201`（创建的结果详情，形状同 GET /v1/backtests/{id}，含 equity_curve/trades）：

```json
{
  "id": 8, "strategy": "buy-hold", "symbol": "DEMO.US",
  "params": {"cash": 10000, "fee": 0, "timeframe": "1d", "adjust": "fwd"},
  "metrics": {"equity": 12100, "total_return": 0.21, "max_drawdown": 0, "bars": 3},
  "start_ts": "2024-06-01T00:00:00Z", "end_ts": "2024-06-03T00:00:00Z",
  "created_at": "2026-08-02T08:00:00Z",
  "equity_curve": [{"ts": "2024-06-01T00:00:00Z", "equity": 10000}],
  "trades": [{"ts": "2024-06-01T00:00:00Z", "action": "buy", "symbol": "DEMO.US", "size": 100, "price": 100, "cash_after": 0}]
}
```

`from_watchlist` 响应 `201`：`{"runs": [<详情>, ...]}`（每条 watchlist 一行）。

| 状态码 | 场景 |
| --- | --- |
| 201 | 执行完成并落库（手填返回详情体；`from_watchlist` 返回 `{"runs": [...]}`） |
| 409 | 已有回测在运行（单进程互斥；`code: busy`，action 提示稍后重试） |
| 422 | 参数校验失败：body 非 JSON、缺 symbol/strategy、未知策略、非法 params、from_watchlist 与显式字段同传、watchlist 为空（`code: invalid_request` / `empty_watchlist`） |
| 503 | 依赖失败：无 bars/期权数据（`no_data`，action 提示先 ingest）、执行超时（`timeout`）、DB/运行失败（`dependency_failed`） |
| 405 | 方法不允许（仅 POST） |

错误体沿用 `{"code", "message", "action", "error"}` 约定；`action` 为可执行的补救建议，例如无数据时（`error` 为兼容别名，值同 `message`）：

```json
{"code": "no_data", "message": "no bars data for HK.00700", "action": "ingest first: `wbot ingest futu -symbol HK.00700 -timeframe 1d`", "error": "no bars data for HK.00700"}
```

CLI 等价物：`wbot backtest -dsn "$WBOT_PG_DSN" -symbol X -strategy Y -params '<json>' -save`（同输入同输出，见 [[BACKTEST]]）。

PRIVACY：本端点不涉及配置值（symbol/strategy/params 为用户业务数据，非凭证；API 永不返回配置值，见 doc/PRIVACY.md）。

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
         "min_ts": "2024-06-01T00:00:00Z", "max_ts": "2024-06-03T00:00:00Z",
         "max_ts_age_seconds": 67290240, "fresh": "stale"}
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
| `data_plane.bars_coverage` | symbol / timeframe / count / min_ts / max_ts / max_ts_age_seconds / fresh | `bars` 表各 symbol×timeframe 组合的条数与 ts 区间；`max_ts_age_seconds` 为 max_ts 距今秒数，`fresh` 为三态新鲜度（fresh/stale/unknown，按 timeframe 默认阈值判定，见 [[DATA_PIPELINE]]）——新字段向后兼容，老客户端忽略即可 |

DB 不可用时（ping 失败）：仍返回 `200`，`db.ok` 为 `false`，且**不执行** pipeline/data_plane 查询——`counts` 全 0、`recent_runs` 与 `bars_coverage` 为空数组（降级语义同 /v1/admin/status；进程字段照常返回）。

ping 通过但存储查询失败时返回 `500`（错误体见 [[#错误]]，`code: internal_error`）。

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
{"code": "dependency_failed", "message": "database unavailable", "action": "check the database connection and retry", "error": "database unavailable"}
```

## GET /v1/futu/quote

实时行情代理（产品组体验意见 7）：浏览器不能直连富途网关（127.0.0.1:22222，CORS/安全），serve 代浏览器先订阅后取 Basic 快照（复用 `internal/futu` 客户端：订阅幂等、限频池内置，见 [[FUTU]] §7/§8）。

Query 参数：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `symbol` | 是 | market 限定 symbol，如 `HK.00700`（前缀支持 `HK./US./SH./SZ.`，非法 → 400） |

网关地址：环境变量 `FUTU_GATEWAY_URL`（默认 `http://127.0.0.1:22222`）；config.yaml 接入后续切片。

响应 `200`：网关 `/api/quote` 的 s2c **原样透传**（代理语义）：

```json
{"basic_qot_list": [{"amplitude": 3.773, "cur_price": 475.2, "high_price": 479.8, "low_price": 462.0, "name": "TENCENT", "open_price": 470.0, "security": {"code": "00700", "market": 1}, "update_time": "2026-07-31 16:07:51", "volume": 31100240}]}
```

| 字段 | 说明 |
| --- | --- |
| `basic_qot_list[0].cur_price` / `open_price` / `high_price` / `low_price` | 现价 / 开盘 / 最高 / 最低 |
| `basic_qot_list[0].volume` | 成交量 |
| `basic_qot_list[0].update_time` | 网关报价时间（+08 墙钟） |
| `basic_qot_list[0].name` / `security` | 标的名称 / 市场枚举 + 代码 |

响应 `503`（网关不可达，连接失败/超时）：`action` 提示启动网关容器——

```json
{"code": "dependency_failed", "message": "Futu gateway unreachable", "action": "start the Futu gateway container (docker compose -f configs/docker-compose.futu.yml up -d) and retry", "error": "Futu gateway unreachable"}
```

响应 `502`（网关已应答但拒绝——HTTP 4xx/5xx 或业务错误如未开通市场权限）：`message` 为网关消息透传（含出错步骤 `subscribe`/`quote`）。

## GET /v1/futu/account

资金 + 持仓只读代理（富途模拟盘账户页，产品切片 ⑤）：浏览器不能直连富途网关（loopback，CORS/安全），serve 代浏览器走 **OpenD protobuf 接口（TCP 11111）** 查询（复用 `internal/futu` 交易客户端 `TradeClient`：Account → Funds/Positions，见 [[FUTU]] §10）。**只读端点**：不下单、不改状态；默认 `sim`（trd_env=0 模拟盘，安全红线默认值），`real` 为只读查询（与 CLI `wbot futu funds|position` 同一安全策略：实盘写操作需老板确认，见 [[FUTU]] 交易安全策略）。

Query 参数：

| 参数 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `env` | 否 | `sim` | 交易环境：`sim`（模拟盘，默认）\| `real`（实盘只读查询；非法值 → 400） |
| `acc_id` | 否 | 该环境第一个账户 | 账户 ID（uint64；非法 → 400） |

网关地址：环境变量 `FUTU_GATEWAY_URL`（与 quote 代理同源；本端点默认 `127.0.0.1:11111` 即 OpenD protobuf 端口）。连接管理：serve 进程内**包级复用**一条 TradeClient 连接（互斥串行；网关自动重连）。

响应 `200`（字段白名单，不泄漏账户/订单元数据；`positions` 空时为空数组）：

```json
{
  "env": "simulate",
  "acc_id": 1907141,
  "funds": {"power": 1198286.822, "total_assets": 1198286.822, "cash": 318666.822, "market_val": 879620, "available_cash": 318666.822},
  "positions": [
    {"symbol": "HK.00700", "qty": 100, "avg_cost": 470.0, "price": 475.2, "market_val": 47520, "pl": 520}
  ]
}
```

| 字段 | 说明 |
| --- | --- |
| `env` / `acc_id` | 账户环境（simulate/real）与账户 ID（标注查询目标，同 CLI 输出约定） |
| `funds.power` / `total_assets` | 购买力 / 资产总额 |
| `funds.cash` / `market_val` / `available_cash` | 现金 / 证券市值 / 可用资金（proto `available_funds`） |
| `positions[].symbol` | market 限定代码（HK./US./SH./SZ.，CN 市场按代码段推断交易所） |
| `positions[].qty` / `avg_cost` / `price` | 数量 / 成本价 / 市价 |
| `positions[].market_val` / `pl` | 市值 / 盈亏金额 |

响应 `503`（网关不可达，连接失败/超时）：`action` 提示启动网关容器（同 `/v1/futu/quote` 约定）。

响应 `502`（网关已应答但拒绝——如 `env` 无匹配账户、trd_env 不匹配、网关业务错误）：`message` 为网关消息透传（含出错步骤 `accounts`/`funds`/`positions`）。

## GET /v1/futu/options

期权链代理（期权链可视化切片）：标的的到期日列表 + 单个到期日的 call/put 链（复用 `internal/futu` 的 `OptionExpirations`/`OptionChain`，快照类限频 1 次/3s 内置，见 [[FUTU]] §8/§10）。浏览器不能直连网关，serve 代浏览器调用。

Query 参数：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `symbol` | 是 | market 限定 symbol，如 `HK.00700`（前缀支持 `HK./US./SH./SZ.`，非法 → 400） |
| `expiry` | 否 | 链到期日 `YYYY-MM-DD`（格式非法 → 400）；缺省为最近未来到期日（`distance_days` ≥ 0 最小者），全部已到期时 `contracts` 为空数组 |

网关地址：环境变量 `FUTU_GATEWAY_URL`（默认 `http://127.0.0.1:22222`，同 quote/account 代理）。

响应 `200`：

```json
{
  "symbol": "HK.00700",
  "expiry": "2026-08-07",
  "expirations": [
    {"date": "2026-07-31", "timestamp": "2026-07-30T16:00:00Z", "distance_days": -1, "cycle": 1},
    {"date": "2026-08-07", "timestamp": "2026-08-06T16:00:00Z", "distance_days": 5, "cycle": 1},
    {"date": "2026-08-28", "timestamp": "2026-08-27T16:00:00Z", "distance_days": 26, "cycle": 1}
  ],
  "contracts": [
    {"expiry": "2026-08-07", "option_type": "call", "strike": 335.0, "symbol": "HK.TCH260807C335000", "lot_size": 100},
    {"expiry": "2026-08-07", "option_type": "put", "strike": 335.0, "symbol": "HK.TCH260807P335000", "lot_size": 100}
  ]
}
```

| 字段 | 说明 |
| --- | --- |
| `expiry` | `contracts` 所属到期日（请求值或默认最近未来；无可用到期时为 `""`） |
| `expirations[].date` | 到期日 `YYYY-MM-DD`（网关 `strike_time`，市场本地日期，权威字段） |
| `expirations[].timestamp` | 到期时刻 RFC3339 UTC（网关 `strike_timestamp`，+08 本地午夜） |
| `expirations[].distance_days` | 距今天数（负 = 已到期） |
| `expirations[].cycle` | 到期周期 |
| `contracts[]` | 该到期日全部行权价的 call/put（按 strike 升序，同 strike call 在前）；`option_type` 为 `call`/`put`，`symbol` 为合约代码（如 `HK.TCH260807C335000`，前缀+到期+Call/Put+行权价×1000），`lot_size` 为每张合约股数 |

**权利金说明**：期权链契约实测（[[FUTU]] §10）不含权利金/隐含波动率——`/api/option-chain` 仅返回合约代码/行权价/lot_size；premium 需逐合约 `option-quote` 或合约 K 线（拉取成本高，P3 排期）。故 `contracts` 无 premium 字段，UI 以合约代码代替。

响应 `503`（网关不可达，连接失败/超时）：`action` 提示启动网关容器（同 `/v1/futu/quote` 约定）。

响应 `502`（网关已应答但拒绝——HTTP 4xx/5xx 或业务错误如未开通市场权限）：`message` 为网关消息透传（含出错步骤 `option-expiration-date`/`option-chain`）。

PRIVACY：本端点无配置值字段（API 永不返回配置值，见 doc/PRIVACY.md）。限频：每请求 2 次快照类调用（到期日 + 链，各 1 次/3s），浏览器轮询需注意。

## 错误

**全量统一约定**（S5，自 `/v1/backtests` S1 引入后全量接入）：所有端点错误体为 `{"code", "message", "action", "error"}`——`code` 为机器可读错误码、`message` 为人类可读描述、`action` 为可执行的补救建议（`invalid_request` → 检查参数重试、`not_found` → 检查路径、`method_not_allowed` → 用文档方法、`internal_error`/`dependency_failed` → 查日志/连接重试）；`error` 为**兼容别名**（值同 `message`），保留给既有客户端（S5 起存量端点也带 `code`/`action`，`error` 字段仍存在，不破坏老消费方）。新客户端优先读 `code`/`message`/`action`。

`{code, message, action, error}` 形态示例：

```json
{"code": "invalid_request", "message": "missing query parameter: symbol", "action": "check the request parameters and retry", "error": "missing query parameter: symbol"}
```

状态码映射（含既有端点）：

| 场景 | 状态码 |
| --- | --- |
| 缺必填参数 / 参数非法（坏时间、limit<=0、空/超长配置值、body 非 JSON、未知策略模板或非法 watchlist 参数） | 400 |
| 存储查询失败 | 500 |
| DB ping 失败 / `/v1/futu/quote`、`/v1/futu/account`、`/v1/futu/options` 网关不可达 | 503 |
| `/v1/futu/quote`、`/v1/futu/account`、`/v1/futu/options` 网关拒绝或业务错误（消息透传） | 502 |
| 未知路径 / 白名单外 config key / DELETE 不存在的 watchlist 标的 / 不存在的 backtest id | 404 |
| 方法不允许（非 GET/PUT/DELETE；watchlist 标的路径仅支持 PUT/DELETE） | 405 |
| POST /v1/backtests：非法参数 422、单进程互斥 busy 409、依赖失败/无数据/超时 503（错误体见上节） | 见上节 |

## 本地验证

```bash
docker compose -f configs/docker-compose.yml up -d
export WBOT_PG_DSN='postgres://postgres:postgres@localhost:5432/wbot_test?sslmode=disable'
wbot ingest mock
wbot serve &
curl -s 'http://127.0.0.1:8080/v1/runs'
curl -s 'http://127.0.0.1:8080/v1/bars?symbol=DEMO.US&timeframe=1d'
curl -s 'http://127.0.0.1:8080/v1/admin/cluster'
curl -s 'http://127.0.0.1:8080/v1/futu/options?symbol=HK.00700'
curl -s 'http://127.0.0.1:8080/v1/futu/options?symbol=HK.00700&expiry=2026-08-07'
curl -s 'http://127.0.0.1:8080/v1/strategies'
curl -X PUT 'http://127.0.0.1:8080/v1/watchlist/HK.00700' -H 'Content-Type: application/json' \
  -d '{"strategy":"covered-call","params":{"strike_pct_otm":0.03}}'
curl -s 'http://127.0.0.1:8080/v1/watchlist'
curl -X DELETE 'http://127.0.0.1:8080/v1/watchlist/HK.00700'
wbot backtest -dsn "$WBOT_PG_DSN" -symbol DEMO.US -adjust none -strategy buy-hold -save
curl -s 'http://127.0.0.1:8080/v1/backtests?symbol=DEMO.US'
curl -s 'http://127.0.0.1:8080/v1/backtests/1'
curl -s 'http://127.0.0.1:8080/v1/backtests/1/export' -o backtest-1.csv
curl -s 'http://127.0.0.1:8080/v1/backtests/1/export?format=json'
curl -X POST 'http://127.0.0.1:8080/v1/backtests' -H 'Content-Type: application/json' \
  -d '{"symbol":"DEMO.US","strategy":"buy-hold"}'
curl -X POST 'http://127.0.0.1:8080/v1/backtests' -H 'Content-Type: application/json' \
  -d '{"from_watchlist":true}'
```

关联：[[DATA_PIPELINE]] [[ROADMAP]]（v4 Go API，微信小程序前置依赖）
