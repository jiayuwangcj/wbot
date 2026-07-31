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

- **status**: `done`（PR #33 已合入 origin/main）
- **last step**: PR #33（feat/admin-status → main）CI 绿、reviewer 评审通过后由主会话合入 origin/main（merge commit `92ef989`）；`doc/API.md` 已含 GET /v1/admin/status 章节。评审遗留 2 项排期前注意事项已转 ⑥-C（见 `doc/tasks/2026-07-31-admin-cluster.md`）：① Pinger 与 Store.Ping 收敛复用（⑥-C 时决定）；② listen_addr 建议注入实际绑定地址 `ln.Addr()` 而非配置值（现 main.go `ListenAddr: *listen`，`-listen 127.0.0.1:0` 会误报）。评审 P2 修复已在 `fix/admin-status-p2`（PR #34 评审中：ping 超时强制 + pctx 断言 ok:false，admin.go/admin_test.go 各 1 文件，未动 main.go）。

## Next

落盘完成。⑥-B（`doc/tasks/2026-07-31-admin-config.md`）与 ⑥-C（`doc/tasks/2026-07-31-admin-cluster.md`）已由 dispatcher 排单（queued，可并行）：各自独立 worktree（admin-config → feat/admin-config；admin-cluster → feat/admin-cluster，base origin/main），端点各入新文件、main.go 顶层 mux 追加注册降低冲突；合入串行由主会话协调。PR #34（fix/admin-status-p2）评审通过后合入，⑥-C 编码时注意不得与其在 admin.go 上冲突（P2 改的是 ping 分支）。
