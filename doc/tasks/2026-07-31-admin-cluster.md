# 后台管理后端 ⑥-C：`GET /v1/admin/cluster` 集群状态 API（单进程组件视图）

- **id**: `2026-07-31-admin-cluster`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

切片 ⑥-C（`doc/issues/draft-2026-07-31-admin-console-api.md` ⑥-C）：新增 `GET /v1/admin/cluster`，**单进程最小语义**组件视图 `components: [进程, DB, 数据管道, 数据面]`——进程复用 ⑥-A 字段（依赖已满足：PR #33 合入 origin/main）、DB 复用 Ping 状态、数据管道复用 `RecentRuns` 聚合（running/succeeded/failed 计数 + 最近 N 条）、数据面 bars 覆盖统计（symbol × timeframe 各组合 count + min/max ts，ingest 新增查询或 Store 新方法）。

## Constraints

- **单进程最小语义，不臆造集群**：wbot 为单进程 CLI；端点命名 `cluster` 仅为对齐老板原话，API.md 明确定义为组件视图，不扩展 master/agent 注册。
- **复用 ⑥-A 进程字段**（draft 依赖条件已满足，不另造等价字段）；DB down 语义沿用 ⑥-A 先例：`200 + db: {"ok": false}`（待拍板 #3 已事实拍板；健康语义归 /v1/health 503）。
- 端点形态：分端点（待拍板 #1 已由 ⑥-A 合入先例事实拍板，不排 overview 聚合壳）。
- **评审注意事项（PR #33 评审，排期前，本切片必须处理）**：
  1. **Pinger 与 Store.Ping 收敛复用（⑥-C 时决定）**——cluster 需要数据面查询 + Ping 双面。默认建议：cluster handler 接受含 Ping 的窄接口（复用 `httpapi.Store.Ping` 或定义新的窄接口），⑥-A 的 `Pinger`/`PingerFunc` 保持不动（避免与评审中的 fix/admin-status-p2 冲突）；若 P2 已合入，则在 admin 包内收敛、不得双定义。决策结果须在 PR 描述/评审里落档。
  2. **listen_addr 注入实际绑定地址（`ln.Addr()`）而非配置值**——现 main.go `meta := ProcessMeta{..., ListenAddr: *listen}` 构造于 `net.Listen` 之前（`-listen 127.0.0.1:0` 会错误上报 `127.0.0.1:0`）。本切片改 main.go 时把 meta 构造移到 `net.Listen` 之后，注入 `ln.Addr().String()`。
- 数据管道聚合：复用 `internal/ingest/status.go` 的 `RecentRuns`；不臆造新的运行语义。
- 数据面覆盖：`internal/ingest` 新增查询（`symbol × timeframe` count、min/max ts，沿用 query.go 的构建方式）或 Store 新方法；不新增 schema。
- **端点注册与并行冲突规避**：建议新文件 `internal/httpapi/admin_cluster.go` + 独立构造函数，main.go 顶层 mux 追加注册（`/v1/admin/cluster` 最长匹配优先于 `/v1/admin/`，admin.go 的 ⑥-A 段零改动）；若并入 admin.go 各段注册，注意与并行任务 ⑥-B 的合入冲突（两任务同改 internal/httpapi/、cmd/wbot/main.go、doc/API.md、main_test.go）。
- 日志前缀 `httpapi:`；`serve -h` 帮助文本同步新端点 + `main_test` 断言。
- 鉴权（待拍板 #5 默认）：默认 127.0.0.1 绑定，不加 token。
- 不引入新依赖（标准库 + 既有 ingest/httpapi）。

## 验收（可测）

- **单测（fake store/注入）**：components 四段字段齐全；进程字段与 ⑥-A 一致（version/pid/started_at/uptime_seconds/listen_addr）；db ok/down 两态（down → ok:false，不 503）；runs 计数（running/succeeded/failed）正确 + 最近 N 条；bars 覆盖（组合 count、min/max ts）正确。
- **集成测**（无 `WBOT_PG_DSN` skip，沿用 `internal/httpapi/integration_test.go` 模式）：真 PG `wbot ingest mock` 后断言 symbol × timeframe 覆盖出现在响应。
- **listen_addr 实际绑定值**：main_test 或 serve 级断言覆盖 `-listen 127.0.0.1:0` 场景上报实际端口（`ln.Addr()`），非配置值。
- Pinger 收敛决策落档（记录选择：保留独立 Pinger / 收敛窄接口 + 理由）。
- `go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`；CI（test + db-integration）绿。
- `doc/API.md` 新增 GET `/v1/admin/cluster` 章节（响应示例 + 单进程组件视图定义 + PRIVACY 说明）；`serve -h` 含新端点（main_test 断言）。
- PRIVACY 扫描零命中（本端点无配置值字段）。

## Links

- Driven-By: `doc/issues/draft-2026-07-31-admin-console-api.md`（⑥-C；依赖 ⑥-A 进程字段——已满足）
- 先例（⑥-A）：`doc/tasks/2026-07-31-admin-status.md`（PR #33 合入 origin/main；AdminHandler/Pinger/ProcessMeta 注入模式）+ PR #33 评审注意事项（Pinger 收敛、listen_addr ln.Addr()）+ `fix/admin-status-p2`（PR #34 评审中：ping 超时强制 + ctx 断言，本切片不得与其冲突）
- 复用：`internal/httpapi/httpapi.go`（Store.Ping、writeError）、`internal/ingest/status.go`（RecentRuns）、`internal/ingest/query.go`（QueryBars 模式）、`internal/ingest/mock.go`（`wbot ingest mock` 集成测）
- 目标切片：`doc/tasks/2026-07-31-miniapp-v1-target.md`（⑥）

## State

- **status**: `queued`
- **last step**: dispatcher 建记录（2026-07-31 off-peak 排单；⑥-A 已合入满足依赖条件）

## Next

主会话：创建 worktree `.claude/worktrees/admin-cluster`（分支 `feat/admin-cluster`，base `origin/main` 最新）→ 派 coder 实现（ingest 覆盖查询 + cluster 端点 + 单测/集成测 → 本地 verify 绿 → push）→ reviewer 独立评审（PRIVACY 扫描、Pinger 收敛复核、ln.Addr 注入断言、单进程语义不臆造）→ CI 绿 → 合入（与 ⑥-B 串行合入，主会话协调 main.go/API.md 冲突）→ 本记录置 done、落盘。
