# （草稿）GitHub Issue 正文 — 后台管理后端（all-in-one 数据面）：运行状态/配置读写/集群状态 API

**建议标题**：`[feature] 后台管理后端数据面：/v1/admin 运行状态 + 配置读写入口 + 集群状态（单进程最小语义）`

---

## Trigger comment

- 仓库级锚点：<https://github.com/jiayuwangcj/wbot/issues/8#issuecomment-4268661869>
- 若单独开 Issue：发帖后在同一 Issue 留锚点评论，把评论 URL 填回此处并作为 PR 的 `Driven-By:`。

## Goal（对齐 `doc/tasks/2026-07-31-miniapp-v1-target.md` 切片⑥，目标 2026-07-31-miniapp-v1）

老板目标（2026-07-31 指令）：除微信小程序外，新增后台管理页面，**all-in-one** 显示后端服务器**运行状态、简要设置、配置、集群状态**；微信/Schwab/IBKR 等凭据**在页面配置**（配置值仍存 `~/.wbot/`，PRIVACY 红线，页面/API 仅提供读写入口）。

**本切片只做后端数据面**——前端页面（go:embed Web UI）属 ROADMAP v4 后续切片，不在本切片。

### 范围（建议的最小拆法：3 个子切片，各自可测可独立合入）

#### 子切片 ⑥-A：运行状态 API（`GET /v1/admin/status`）

进程级 + DB 级运行状态：

- 进程：`version`（复用 main 包 ldflags 注入的 `version` 变量）、`pid`、`started_at`、`uptime_seconds`、`listen_addr`（来自 serve 启动参数）。
- DB：`ok`（复用 Store.Ping，≤3s 超时）+ `latency_ms`（可选，ping 耗时，不加超时风险）。
- 端点挂 `/v1/admin/` 命名空间，与小程序只读 API（/v1/bars、/v1/runs、/v1/health）隔离；`internal/httpapi` 新增文件（建议 `admin.go`）注册，避免与既有单文件 handler 冲突面。
- 进程元信息（version/started_at/listen_addr）由 `serve` 注入 handler（构造参数或 `Handler` 变体），单测可传 fake 值。

#### 子切片 ⑥-B：配置读写入口（`internal/config` + `GET /v1/admin/config` / `PUT /v1/admin/config/{key}`）

- **新建 `internal/config` 包**（仓库已有空目录占位，git 未跟踪）：管理 `~/.wbot/` 下的配置文件（建议 `wbot.conf`，JSON，0600，原子写 tmp+rename）；测试注入 tmpdir 而非真实 home。
- key 分组建议（**最终清单待拍板**）：
  - `credentials.wechat.*`（appid / secret / token）
  - `credentials.schwab.*`（api_key / account 等）
  - `credentials.ibkr.*`（gateway_host / gateway_port / account 等）
  - `system.*`（简要设置：如 `listen` 等，供后续）
- `GET /v1/admin/config`：返回 `[{key, group, set: bool, updated_at}]`——**永不返回配置值**（PRIVACY 红线：值只进 `~/.wbot/`，API 只提供读写入口契约）。
- `PUT /v1/admin/config/{key}`：body `{"value": "..."}`；校验（key 合法、值非空、长度上限）；持久化到 `~/.wbot/`（0600）；响应不含值（如 `{"key": "...", "set": true}`）。

#### 子切片 ⑥-C：集群状态 API（`GET /v1/admin/cluster`）

**单进程最小语义**（wbot 为单进程 CLI，无真实集群，不得臆造）：响应为组件视图 `components: [进程, DB, 数据管道, 数据面]`：

- 进程：复用 ⑥-A 的进程字段（依赖 A 先合入，或本切片含等价字段）。
- DB：复用 Ping 状态。
- 数据管道：最近 runs 聚合（`ingestion_runs`：running/succeeded/failed 计数 + 最近 N 条，复用 `RecentRuns`）。
- 数据面：bars 表覆盖统计（`symbol × timeframe`、各组合 count、min/max ts）——`internal/ingest` 新增查询或 Store 新方法。
- 端点命名 `cluster` 仅为对齐老板原话；文档明确定义为单进程组件视图。

### 验收（每子切片均须满足）

- `go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`；CI（test + db-integration）绿。
- **⑥-A**：httptest 单测（fake store：字段齐全、db ok/down 两态）；集成测（有 `WBOT_PG_DSN` 时真 PG 断言 `db.ok==true`，无则 skip，沿用 `internal/httpapi/integration_test.go` 模式）。
- **⑥-B**：config 包单测（tmpdir：写读、权限 0600、非法 key/空值拒绝、原子写）；httptest 契约测（GET 响应体**不含**已写入的测试值——泄漏断言；PUT 成功 → GET `set: true`；非法 key 400；空值 400）；CI db-integration 不依赖 PG（config 不走 DB，仅单测 + verify 即可，另加 CI 静态检查/测试断言无真实值入 diff）。
- **⑥-C**：单测（fake store 各组件字段）；集成测（真 PG：`wbot ingest mock` 后断言 symbol 覆盖出现在响应）。
- `doc/API.md` 增加 `/v1/admin/*` 三端点章节（响应示例 + PRIVACY 说明「API 永不返回配置值」）；`wbot serve -h` 帮助文本同步（`main_test` 断言）。

### 非目标

- **前端页面**（go:embed Web UI → ROADMAP v4 后续切片；本切片仅数据面）。
- 配置**值**入库/回显/进日志（PRIVACY 红线，reviewer 必查）。
- 真实集群/多节点编排、master/agent 注册扩展（现为单进程占位 smoke，不扩展）。
- 凭证的真实接入与校验（Schwab/IBKR 属 v3；微信 token 属切片⑦——老板提供资源，见 discussions/21，保持挂起）。
- 鉴权体系（默认绑定 127.0.0.1 即安全边界；管理 API 加 token 属后续待拍板）。

### 待拍板项

1. **端点形态**：分端点（status/config/cluster 三个）vs 聚合单端点（如 `GET /v1/admin/overview`）——产品建议：分端点 + 前端自行聚合（前端切片再定），或 overview 仅作聚合壳。
2. **⑥-B key schema 最终清单**（微信/Schwab/IBKR 字段名与分组）；配置文件格式（JSON `wbot.conf` vs 追加写 `env.sh`——产品建议 JSON，env.sh 由运维手动维护，避免双写竞态）。
3. **DB down 时 ⑥-A/⑥-C 语义**：200 + `db: {"ok": false}`（信息端点，健康语义仍归 /v1/health 503）vs 503——产品建议前者。
4. **⑥-B 写入生效语义**：本切片仅持久化（静态读写入口）；运行时热加载/生效由各消费方（ingest/serve 启动时读）另行定义——建议本切片只保证「落盘 + 可读回」，生效归后续切片。
5. 管理 API 是否加鉴权 token（默认 127.0.0.1 绑定，不做）。

## 依赖

- ⑥-A：无（复用 internal/httpapi 的 Store/Ping 模式；依赖已完成的切片①③：/v1/bars、/v1/runs、/v1/health）。
- ⑥-B：新建 `internal/config`（空目录占位）；PRIVACY 约定（doc/PRIVACY.md）；不依赖 DB。
- ⑥-C：⑥-A（进程状态，或含等价字段独立）；ingest 表 `bars`/`ingestion_runs`（001/002 migrations 已存在）。
- 建议派单顺序：**⑥-A → ⑥-C（可与 B 并行）→ ⑥-B**；并行注意：三个子切片都改 `internal/httpapi`——**新端点放新文件 `internal/httpapi/admin.go`**（A/B/C 各一段注册），降低 worktree 冲突；`internal/config` 仅 ⑥-B 独占。
- 切片⑦（真实凭证值配置落地）保持挂起：依赖老板提供微信 token 等（discussions/21）；⑥-B 完成后 ⑦ 解除 API 侧阻碍。

## Plan（可勾选）

- [ ] ⑥-A：`internal/httpapi/admin.go` 进程/DB 状态端点 + 注入方式 + 单测/集成测 + API.md
- [ ] ⑥-B：`internal/config` 包（tmpdir 单测）+ config 端点 + 泄漏断言测试 + API.md
- [ ] ⑥-C：ingest bars 覆盖查询 + cluster 端点 + 单测/集成测 + API.md
- [ ] serve 帮助文本/`main_test` 同步；`scripts/verify.sh` 与 CI 保持绿；reviewer PRIVACY 扫描（值不进 diff）

## 仓库内链回

- 目标切片：`doc/tasks/2026-07-31-miniapp-v1-target.md`（⑥⑦）
- 现状复用：`internal/httpapi/httpapi.go`（Store 接口 + writeError）、`internal/ingest/status.go`（RecentRuns）、`internal/ingest/query.go`（QueryBars）、`cmd/wbot/main.go` runServe（-listen/-dsn/-duration）
- 契约：`doc/API.md`；红线：`doc/PRIVACY.md`（~/.wbot/）
- 前置切片轨迹：`doc/tasks/2026-07-31-httpapi-serve.md`、`doc/tasks/2026-07-31-miniapp-health.md`
- 前端路线：`doc/ROADMAP.md` v4（go:embed Web UI）；需求源：discussions/10、discussions/21
