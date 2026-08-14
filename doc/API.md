# API 契约（wbot serve）

本文件描述当前产品契约：每个 watchlist 标的只有一个结构化 `wheel` 配置，策略输出只有人工可读的 `ALERT` 或 `HOLD`。Wheel runner 不自动下单；通过 LLM 闸门后，已配置 Telegram 的人工 `yes` 仅允许模拟环境下单，缺失行情仍只能 `HOLD`。

`wbot serve` 默认监听 `127.0.0.1:8080`，数据源由 `-dsn` 或 `$WBOT_PG_DSN` 提供。服务默认无鉴权，仅适合本机或由用户自行加认证的反向代理；账户数据和写接口不得直接暴露公网。

## 实时 Wheel 与 Telegram

`serve -wheel-run` 启动实时 Wheel runner，`-wheel-interval` 控制轮询周期（默认 5 分钟），`-wheel-env sim|real` 选择账户环境；runner 读取 `$FUTU_GATEWAY_URL` 的 REST 行情、`$FUTU_PROTO_ADDR` 的账户/持仓与期权链，并将每个标的的信号和执行状态写入 PostgreSQL。网关不可用或报价快照不完整时保持 `HOLD`/`DATA_BLOCKED`，服务本身仍保持健康。

ALERT 只有在配置完整的 LLM 审核器后才进入提醒链路。`LLM_BASE_URL` 是 OpenAI-compatible API 的 base URL（客户端追加 `/chat/completions`），`LLM_API_KEY` 和 `LLM_MODEL` 分别提供认证 key 与模型名；三者任一缺失时，开启 `-wheel-run` 的 serve 会打印一行 warning，ALERT 不会推送。key 只从环境变量读取，不写入数据库或日志。

`serve -telegram-run` 单独启动 Telegram 轮询：从 `~/.wbot/wbot.conf` 读取 `credentials.telegram.token` 与 `credentials.telegram.chat_ids`，只推送 LLM `APPROVE` 的 ALERT，并提供 `yes`/`no`/`今日不再提醒` 处置按钮。`yes` 只走 sim 账户；dismiss 按 symbol 和 UTC 当日写入静默记录。测试或受控环境可用 `WBOT_CONFIG_DIR` 指定配置目录、`TELEGRAM_API_BASE_URL` 指定兼容 Telegram API。

## 产品边界和能力状态

| 能力 | 状态 | 阻塞原因 | 启用闸门 | 禁止降级 |
| --- | --- | --- | --- | --- |
| `/v1/strategies` 单一 Wheel schema | `READY`（领域/注册表） | 领域单测和 schema 校验已通过 | 保持 JSON round-trip 和错误契约回归 | 不恢复旧策略模板 |
| `/v1/watchlist` Wheel 配置写入 | `READY`（服务端校验） | 迁移后的旧行仍可能没有完整配置 | 新建版本配置并通过 UI/DB 验收 | 不从历史参数猜曲线或最大库存 |
| 原子期权报价 snapshot schema/repository | `READY` | `005_wheel` migration、不可变写入/查询和缺字段留痕已实现并有单测 | 保持 append-only、字段边界和 snapshot key 原子性 | 不把不完整记录当作可执行报价 |
| 真实供应商报价 adapter | `DATA_BLOCKED` | 尚无经过验收的供应商字段映射，无法证明同一时点完整 bid/ask、Delta、IV、Theta、OI、volume、lot size 和 freshness | 可信只读 adapter、真实采样、原子性、断线/限流/陈旧测试全部通过 | 不用日线收盘、固定 Delta/IV/OI、默认 Theta 或拼接不同时间数据 |
| Wheel `ALERT` 提醒 | `DATA_BLOCKED` | `bid/ask/Delta/IV/OI/Theta` 等可信快照覆盖不足 | snapshot 新鲜度和完整性闸门通过 | 缺字段只能 `HOLD` |
| 配置/快照/信号/人工动作持久化 | `READY`（repository） | 版本配置、不可变 snapshot、`ALERT/HOLD` fail-closed 校验和 append-only action repository 已实现，真实 PostgreSQL 集成已通过 | 生产 DB migration/PG 集成证据持续纳入发布验收 | 不覆盖历史版本，不提供自动执行路径 |
| 人工确认/忽略/成交回填 | `INTEGRATION_BLOCKED` | actor 身份、权限和审计 UI 尚未验收 | 鉴权、append-only 审计和浏览器流程完成 | 不把人工确认变成下单，不接受无 actor 的动作 |
| 浏览器结构化编辑和只读审计流程 | `READY` | Mac Chrome desktop/390px、动态 detail、watchlist 空态和批量/重跑表单已验证 | 保持 DOM/CDP 与契约回归 | 不把只读证据扩展为人工写动作 READY |
| 当前 bar-time snapshot 回放 | `READY`（研究/验证） | bars 运行器已按 bar 选择最新原子 snapshot，但不代表事件级历史执行 | 保持同输入同 trace、原子批次不混用和 stale → HOLD | 不宣称为事件驱动回测或实时执行 |
| 历史事件回测 | `DATA_BLOCKED` | 现有历史覆盖不足以还原逐 quote/成交事件及同一时点盘口/Greeks | 历史原子 snapshot 覆盖目标日期/DTE，事件 trace 可复现并通过数据质量验收 | 不用 OHLC 猜 bid/ask/Greeks，不把 bar-time 回放冒充事件回测 |
| 实时/自动执行 | `OUT_OF_SCOPE` | 产品只做人工提醒，不提供实时执行器 | 永久不接交易 API | 无自动确认、无实时执行器、无隐式降级开关 |

状态值 `READY`、`DATA_BLOCKED`、`INTEGRATION_BLOCKED`、`RESEARCH_ONLY`、`OUT_OF_SCOPE` 表示能力闸门。`NEEDS_RECONFIGURATION` 已废弃：S1 有损迁移后旧行自动映射满仓/清仓价格，无需用户重提交完整配置；迁移行通过配置 `params` 内的 `migration_lossy=true`、`migration_warnings` 和 `migration_original_price_position_curve` 审计字段展示。任何阻塞响应都应携带 `capability_status`、`blocked_by`、缺失字段和下一启用条件。

## Web UI

| 路径 | 行为 |
| --- | --- |
| `GET /` | 301 到 `/ui/` |
| `GET /ui/` | Dashboard：账户只读摘要、资产快照曲线、持仓/订单只读信息 |
| `GET /ui/watchlist.html` | Wheel 配置编辑：曲线、最大库存、DTE、质量、频率和战略状态 |
| `GET /ui/results.html` | 回测结果、capability/snapshot trace 和导出入口；Mac Chrome desktop/390px 已验证 |
| `GET /ui/data.html` | bars、数据覆盖和期权数据新鲜度 |
| `GET /ui/admin.html` | 进程/DB/数据管道状态和配置 key 写入（不回显配置值） |

## GET /v1/strategies

只读返回唯一策略及结构化参数 schema。`hold`/`buy-hold` 若仍被底层回测运行器接受，只是内部 benchmark；不出现在本端点，也不属于 watchlist 产品配置。

响应 `200` 的核心形状：

```json
[
  {
    "name": "wheel",
    "description": "按满仓价—清仓价区间管理库存，只生成人工提醒，不自动下单",
    "params": [
      {"name":"full_position_price","type":"number","required":true},
      {"name":"zero_position_price","type":"number","required":true},
      {"name":"max_inventory","type":"number","required":true},
      {"name":"move_interval_pct","type":"number","default":0},
      {"name":"min_premium_per_share","type":"number","default":0},
      {"name":"stock_switch_pct","type":"number","default":0},
      {"name":"trade_gap","type":"number","default":50},
      {"name":"min_dte","type":"number","default":5},
      {"name":"max_dte","type":"number","default":10},
      {"name":"min_option_quality","type":"number","default":0.6},
      {"name":"strategic_state","type":"choice","default":"NORMAL","choices":["NORMAL","CAUTION","PAUSE_BUY","EXIT"]}
    ]
  }
]
```

满仓价必须大于 0，清仓价必须大于满仓价，最大库存为正整数；DTE 必须位于 5–10，质量分在 `[0,1]`，其余战术参数非负。百分比输入使用小数（`0.018` 表示 `1.8%`）。新战术键可省略且 0 表示关闭相应门槛；策略不设每日提醒次数上限。缺少三个 required 字段、未知字段、类型或范围非法时返回 `400 invalid_request`。

## GET /v1/watchlist

按 symbol 升序返回 watchlist。响应中的 `strategy` 必为 `wheel`，`params` 是该标的已校验 Wheel 配置（至少含两个 required 字段；可选字段按 schema 默认值解释）；`created_at`/`updated_at` 为 RFC3339。

```json
[
  {
    "symbol":"HK.00700",
    "strategy":"wheel",
    "params": {
      "full_position_price":400,
      "zero_position_price":550,
      "max_inventory":1200,
      "move_interval_pct":0.018,
      "min_premium_per_share":1.2,
      "stock_switch_pct":0.03,
      "trade_gap":50,
      "min_dte":5,
      "max_dte":10,
      "min_option_quality":0.6,
      "strategic_state":"NORMAL"
    },
    "created_at":"2026-08-10T01:00:00Z",
    "updated_at":"2026-08-10T01:00:00Z"
  }
]
```

旧曲线配置可读取并转换为新键；转换后的新版本带 `migration_lossy`、原曲线审计值和迁移告警计数，持久化不再写旧参数键。

## PUT /v1/watchlist/{symbol}

新增或更新一个标的。新请求必须显式传 `full_position_price`、`zero_position_price` 与 `max_inventory`；其余字段可使用 `/v1/strategies` 中的文档默认值。兼容读取旧曲线请求，但持久化统一写新键。当前 serve 路由负责 schema 校验、追加 `wheel_configs(symbol, version)` 不可变版本并把 watchlist 指向新版本；它不会覆盖已产生信号引用的版本。

```bash
curl -X PUT 'http://127.0.0.1:8080/v1/watchlist/HK.00700' \
  -H 'Content-Type: application/json' \
  -d '{"strategy":"wheel","params":{"full_position_price":400,"zero_position_price":550,"max_inventory":1200,"move_interval_pct":0.018,"min_premium_per_share":1.2,"stock_switch_pct":0.03,"trade_gap":50,"min_dte":5,"max_dte":10,"min_option_quality":0.6,"strategic_state":"NORMAL"}}'
```

成功返回 `200` 和存储后的 watchlist 行。`400 invalid_request` 覆盖缺 symbol/strategy、strategy 不是 `wheel`、缺 required 字段、非法曲线、未知字段、类型/范围错误或非 JSON body；`405` 表示方法不允许。`DELETE /v1/watchlist/{symbol}` 只删除关注绑定，不删除配置/快照/信号审计；不存在返回 `404`。

## Snapshot、信号和人工动作契约

当前 serve 路由已提供三个只读审计端点，不提供配置、信号或人工动作的 HTTP 写入：

- `GET /v1/wheel/configs?symbol=&limit=`：按 symbol 读取不可变配置版本。
- `GET /v1/wheel/signals?symbol=&action=ALERT|HOLD&capability=READY|DATA_BLOCKED&limit=`：读取信号、能力状态、阻塞依赖、库存和候选；`capability` 过滤能力状态（默认全部），`READY` 表示可提醒、`DATA_BLOCKED` 表示数据缺失被阻塞，非法值 `400`。
- `GET /v1/wheel/signals/{id}/actions`：读取该信号已有的人工处置记录。

时间统一 RFC3339 UTC，空集合返回 `[]`；非法查询为 `400`，未知路径为 `404`，非 GET 为 `405`。这些端点只依赖窄化的 read-only store。HTTP/API 与 UI 仍只读，不提供人工动作写入、身份认证或授权端点；处置闭环由受配置 chat ID 限制的 Telegram runner 追加审计记录。底层持久化契约如下：

| 表 | 必须保留 |
| --- | --- |
| `wheel_configs` | symbol、正整数 version、完整配置、战略状态 JSON、创建时间；版本不可变 |
| `option_quote_snapshots` | underlying/contract、PUT/CALL、strike/expiry、source、snapshot key、underlying price、Delta、bid/ask、IV、Theta、volume、OI、lot size、observed/ingested time；允许缺字段留痕，但不可用于 `ALERT` |
| `wheel_signals` | `ALERT`/`HOLD`、config version、capability status、blocked_by、完整库存 snapshot、候选、拒绝理由和 reason |
| `wheel_signal_actions` | `LLM_REVIEW` 及人工 `CONFIRM`/`IGNORE`/`FILL`/`NOTE`/`NO`/`REJECTED`、actor、备注和详情；append-only |
| `wheel_signal_dismissals` | symbol、UTC 当日和唯一键；Telegram 的“今日不再提醒”按日生效 |

库存 snapshot 至少包括 `current_price`、`actual_inventory`、`option_delta_stock`、`effective_inventory`、`target_inventory`、`inventory_gap`，并能追溯 stock shares、futures-equivalent shares 和期权腿。完整报价字段缺任一项、时间不一致、过期、倒挂或不满足 Delta/流动性边界时，信号必须是：

```json
{
  "action":"HOLD",
  "capability_status":"DATA_BLOCKED",
  "blocked_by":["bid","ask","delta","implied_vol","open_interest","quote_time"],
  "reason":"snapshot incomplete; no alert generated"
}
```

只有 `READY`、完整库存、至少一个通过风控的候选才允许 `ALERT`；系统永不自动调用交易 API。LLM 审核与 Telegram 人工处置只追加审计记录；Telegram `yes` 是受 chat ID 限制的 sim 环境人工操作，不重算或覆盖历史信号，HTTP/UI 仍不能写动作。

## 回测结果端点

这些端点是通用结果/导出数据面；产品 Wheel 回测能力由数据质量闸门决定。当前运行器是 bar-time replay：每根 bar 选择 `observed_at <= bar.ts` 的最新原子 snapshot；同一时点按 `futu` → `hkex` → 其他 source 字典序，再按 snapshot key 稳定取值。HKEX 日终投影即使完整也只能是 `RESEARCH_ONLY`，不是事件驱动的 quote/成交回放。

### GET /v1/backtests

读取 `backtest_results` 摘要，默认最新在前；支持 `symbol`、`strategy`、`q`、`offset`、`limit`、白名单 `sort/order`。列表字段为 `id`、`strategy`、`symbol`、`params`、`metrics`、`start_ts`、`end_ts`、`created_at`，不含完整曲线。新客户端只应筛选 `strategy=wheel`；内部 benchmark 结果不代表产品策略。

### GET /v1/backtests/{id}

返回摘要加 `equity_curve`、`trades` 和逐 bar `signals`；`params.strategy_params` 保留完整 Wheel 运行配置。每条新信号 trace 包含 `capability_status`、`blocked_by`、`snapshot_key`、`snapshot_observed_at`、实际/有效库存和期权 Delta 库存，可区分数据阻塞 HOLD 与风险 HOLD。时间统一 RFC3339 UTC `Z`。人工动作和 watchlist `config_version` 仍属于独立审计表，不在回测详情中。不存在为 `404 not_found`，非法 id 为 `400 invalid_request`，非 GET 为 `405`。

### GET /v1/backtests/{id}/export

`format=csv|json`，默认 CSV；JSON 与详情使用同一序列化器，CSV 分为 `equity_curve`、`trades`、`signals` 三段。不存在/非法格式分别返回 `404`/`400`。

### POST /v1/backtests

产品请求形态为 `{"symbol":"...","strategy":"wheel","params":{完整配置}}`，或 `{"from_watchlist":true}` 批量运行 watchlist 中的 Wheel 配置；该产品端点拒绝 `hold`/`buy-hold`。完全没有 bars、依赖故障或超时返回 `503` 和结构化错误；零 snapshot 行或 snapshot 报价不完整、陈旧、缺少所需 Put/Call 方向时，bar-time 研究回放仍保存并返回 `201`，但对应 trace 必须全程或逐 bar 为 `DATA_BLOCKED/HOLD`，metrics 的 `data_quality` 明确给出零覆盖/缺字段，不能产生假 `ALERT`。因此 `201` 表示阻塞证据已持久化，不表示实时提醒能力已解锁。同进程互斥时返回 `409 busy`。CLI 内部仍可运行 `hold`/`buy-hold` 基准，但客户端不得把它们当作 Wheel 产品能力。

## 其他只读数据面

下列既有端点不改变 Wheel 的提醒边界：

`GET /v1/bars?symbol=&timeframe=&adjust=&from=&to=&limit=&desc=1` 返回 OHLCV bars；默认按时间升序，`desc=1` 时最新在前。若同一 `symbol/timeframe/adjust/ts` 存在多个来源，响应只保留一条，并按 `futu` → `tencent` → 其他 `source` 字典序确定性择一。每条 bar 除 `ts/open/high/low/close/volume` 外还返回 `source` 和 `adjusted`：`source` 是实际选中的平台（如 `futu`、`tencent`），`adjusted` 是该平台的复权语义（例如 Tencent canonical `adjust=fwd` 返回 `qfq`），客户端不得仅凭请求的 `adjust` 猜测来源或供应商复权名称。

| 端点 | 语义 |
| --- | --- |
| `GET /v1/health` | DB ping，失败 `503 dependency_failed` |
| `GET /v1/datacheck` | watchlist bars/期权覆盖快照，只读，不 repair |
| `GET /v1/runs`、`GET /v1/bars` | ingestion runs 和带逐 bar `source`/`adjusted` provenance 的 OHLCV bars |
| `GET /v1/account/snapshots` | DB 中的账户资金历史，不走交易网关 |
| `GET /v1/futu/quote`、`/v1/futu/options` | 网关行情/链只读代理，不能作为完整 Wheel snapshot 的替代 |
| `GET /v1/futu/account`、`/v1/futu/orders` | 账户/订单只读代理，不提供下单或撤单 |
| `POST /v1/ingest` | 数据管道写入；不能把日线期权数据宣称为实时 snapshot |
| `GET/PUT /v1/admin/config`、`GET /v1/admin/status`、`GET /v1/admin/cluster` | 管理状态/配置 key；永不回显配置值 |

## 错误契约

serve 错误体统一为：

```json
{"code":"invalid_request","message":"...","action":"check the request and retry","error":"..."}
```

`error` 是兼容别名；新客户端读取 `code`/`message`/`action`。常见状态码：`400` 参数或配置非法，`404` 路径/资源不存在，`405` 方法不允许，`409` 运行器忙，`422` 批量请求互斥或语义非法，`500` 存储错误，`502/503` 网关或依赖失败。阻塞不是成功：对 Wheel 信号必须显式返回/持久化 `HOLD`、`capability_status`、`blocked_by` 和启用条件。

## 本地验证

```bash
docker compose -f configs/docker-compose.yml up -d
export WBOT_PG_DSN='postgres://postgres:postgres@localhost:5432/wbot_test?sslmode=disable'
go test ./internal/wheel ./internal/strategy ./internal/wheelstore ./internal/httpapi
wbot ingest mock
wbot serve &
curl -s http://127.0.0.1:8080/v1/strategies
curl -s http://127.0.0.1:8080/v1/watchlist
curl -s http://127.0.0.1:8080/v1/datacheck
curl -s http://127.0.0.1:8080/v1/backtests?strategy=wheel
```

迁移说明：历史资料中可能出现 `covered-call`、`cash-secured-put` 等旧名称；它们仅用于识别旧行、审计和迁移说明，不是本 API 的合法 strategy，也不能出现在新示例、watchlist 或提醒流程中。

关联：[[BACKTEST]] [[DATA_PIPELINE]] [[DATA_STANDARD]] [[FUTU]] [[PRIVACY]] [[WHEEL_STRATEGY]]
