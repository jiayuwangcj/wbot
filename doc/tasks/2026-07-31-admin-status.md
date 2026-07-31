# 后台管理后端 ⑥-A：`GET /v1/admin/status` 运行状态 API

- **id**: `2026-07-31-admin-status`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

切片 ⑥-A（后台管理后端数据面第 1 刀，`doc/issues/draft-2026-07-31-admin-console-api.md` ⑥-A）：新增 `GET /v1/admin/status`，返回进程级（`version` 复用 main 包 ldflags 注入变量、`pid`、`started_at`、`uptime_seconds`、`listen_addr` 来自 serve 启动参数）+ DB 级（`ok` ≤3s 超时 Ping、`latency_ms` 可选）运行状态；端点挂 `/v1/admin/` 命名空间；进程元信息由 serve 注入（构造参数或 Handler 变体，单测可传 fake 值）。

## Constraints

- 新端点放**新文件 `internal/httpapi/admin.go`**（不与既有 httpapi.go 的 Handler 单文件冲突面）；不改既有端点语义；只读；不引入新依赖（标准库 net/http）；不改 schema。
- **DB down 语义：200 + `db: {"ok": false}`**（默认取产品建议——信息端点，健康语义仍归 /v1/health 503；待拍板项 #3，评审时复核，勿在代码里写死理由之外的 503）。
- **Ping 复用与冲突规避**：/v1/health（含 `Store.Ping`）在 `fix/miniapp-health-p2` 评审中、未合入 main。admin.go 内定义等价小接口（`Ping(ctx context.Context) error`，签名与 health 分支一致）或接受注入的 Ping 函数，**不修改 httpapi.go 的 Store 接口**；若编码期间 health 已合入 main，则直接复用其 `Store.Ping`，不得双定义。
- **PRIVACY 红线**（doc/PRIVACY.md）：status 响应不包含任何配置值/凭据（本切片无凭据字段）；diff 无真实值。
- **日志模块前缀 `httpapi:`**（沿用 serve 既有 `httpapi: listening on http://...` 风格；新增日志沿用此前缀）。
- `serve -h` 帮助文本同步新端点说明；`main_test` 断言。
- 待拍板项 #1/#2/#4/#5 均不影响本切片（端点形态按分端点；B 的 key 清单/生效语义/鉴权均不排）。

## Links

- Driven-By: `doc/issues/draft-2026-07-31-admin-console-api.md`（PR #31 评审中；⑥ 拆 A/B/C，建议顺序 A 先行 → C 与 B 并行；本任务只做 A）
- 目标切片：`doc/tasks/2026-07-31-miniapp-v1-target.md`（⑥）
- 前置切片轨迹：`doc/tasks/2026-07-31-httpapi-serve.md`（done：/v1/bars、/v1/runs）；切片③ /v1/health 在 `fix/miniapp-health-p2` 评审中（未合入 main）
- 复用：`internal/httpapi/httpapi.go`（writeError/JSON 错误体风格）、`cmd/wbot/main.go` `runServe`（`-listen` 默认 127.0.0.1:8080、`var version` ldflags 注入、`httpapi: listening on` 日志）
- 红线：`doc/PRIVACY.md`；契约：`doc/API.md`（现有 GET /v1/runs、GET /v1/bars、错误、本地验证章节）

## State

- **status**: `running`（实现完成，待 CI/评审/合入）
- **last step**: coder 完成并 push commit `7e5bb88`（`feat/httpapi: add GET /v1/admin/status runtime status endpoint`）至 `origin/feat/admin-status`。改动：`internal/httpapi/admin.go`（新增，`AdminHandler(meta ProcessMeta, pinger Pinger)`，Pinger 小接口 + PingerFunc 适配，未改 httpapi.go 的 Store 接口）、`admin_test.go`（httptest：字段齐全/db ok/down 两态/405/404/3s 超时断言/组合路由）、`admin_integration_test.go`（真 PG 断言 db.ok==true，无 DSN skip）、`cmd/wbot/main.go`（runServe 注入 ProcessMeta、顶层 mux 挂 /v1/admin/）、`main_test.go`（serve -h 输出含 /v1/admin/status）、`doc/API.md`（新章节 + PRIVACY 说明）。本地验证（coder 报告）：`go test ./... -count=1` 13 包绿、`go vet ./...` 干净、`scripts/verify.sh` → `verify: ok`、PRIVACY 扫描零命中；集成测本地无 PG 未跑（留 CI db-integration）。dispatcher 已核验：commit 已 push、diff 6 文件 354+/2-、无越界文件。

## Next

主会话：开 PR（feat/admin-status → main，Driven-By 锚点）→ CI（test + db-integration）绿 → reviewer 独立评审（PRIVACY 扫描必查、待拍板项 #3 DB down 语义复核 200+ok:false、Pinger/health 合入兼容）→ 合入 → 本记录置 done、落盘。之后按 draft 建议派 ⑥-C 与 ⑥-B 并行（新端点继续入 admin.go 各段注册）。
