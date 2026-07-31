# 后台管理后端 ⑥-B：`internal/config` 配置读写入口（GET /v1/admin/config + PUT /v1/admin/config/{key}）

- **id**: `2026-07-31-admin-config`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

切片 ⑥-B（`doc/issues/draft-2026-07-31-admin-console-api.md` ⑥-B）：新建 `internal/config` 包（管理 `~/.wbot/wbot.conf`，JSON，0600，原子写）+ 两个端点：`GET /v1/admin/config` 返回 `[{key, group, set, updated_at}]`（**永不返回配置值**），`PUT /v1/admin/config/{key}` 校验后落盘（响应不含值）。为切片⑦（真实凭证值配置落地）解除 API 侧阻碍。

## Constraints

- **新建 `internal/config` 包**：origin/main 无此目录（仅本地空目录占位、git 未跟踪）；测试注入 tmpdir，不读真实 home。
- **配置文件格式默认 JSON `wbot.conf`**（待拍板 #2 产品建议：JSON + 0600 + tmp+rename 原子写；`env.sh` 追加写由运维手动维护，避免双写竞态）——实现中选此默认，记录到 API.md；如老板改拍板 env.sh，仅改持久化层。
- **PRIVACY 红线**（doc/PRIVACY.md）：API 永不返回配置值——GET 只回 key 清单元数据、PUT 响应不含 value；diff 无真实值；reviewer 必查（泄漏 → P0）。
- **key 白名单校验（PR #31 评审 P2，必进验收）**：PUT 只接受白名单内 key，白名单外 → 404；key 非空且长度上限、value 非空且长度上限 → 400。默认白名单（待拍板可演进，实现内建常量表 + 单测覆盖）：`credentials.wechat.{appid,secret,token}`、`credentials.schwab.{api_key,account}`、`credentials.ibkr.{gateway_host,gateway_port,account}`、`system.{listen}`。
- **写后生效语义（待拍板 #4 默认）**：本切片仅「落盘 + 可读回」；运行时热加载/生效由消费方（ingest/serve 启动时读）另行定义，不排。
- **不依赖 DB**：config 不走 PG（CI 无需 db-integration，仅单测 + verify + CI test 即可）。
- **端点注册与并行冲突规避**：建议新文件 `internal/httpapi/admin_config.go` + 独立构造函数（如 `ConfigHandler(cfg *config.Store)`），main.go 顶层 mux 追加注册（Go ServeMux 最长匹配，`/v1/admin/config` 优先于 `/v1/admin/`，admin.go 的 ⑥-A 段零改动）；若并入 admin.go 各段注册，注意与并行任务 ⑥-C 的合入冲突（两任务同改 internal/httpapi/、cmd/wbot/main.go、doc/API.md、main_test.go）。
- 日志前缀 `httpapi:`（沿用既有风格）；`serve -h` 帮助文本同步新端点 + `main_test` 断言。
- 鉴权（待拍板 #5 默认）：默认 127.0.0.1 绑定，不加 token。
- 不引入新依赖（标准库即可）。

## 验收（可测）

- **config 包单测（tmpdir）**：写读回一致；文件权限 0600 断言；白名单外 key 拒绝、空值/超长值拒绝；原子写（tmp+rename，无半写残留）；重复写覆盖。
- **httptest 契约测**：GET 响应体**不含**已写入的测试值（泄漏断言）；PUT 成功 → GET 该 key `set: true`；白名单外 key → 404；空值 → 400；非 GET/PUT → 405；PUT 响应不含 value。
- `go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`；CI（test）绿。
- `doc/API.md` 新增 GET/PUT `/v1/admin/config` 章节（响应示例 + PRIVACY 说明「API 永不返回配置值」）；`serve -h` 含新端点（main_test 断言）。
- PRIVACY 扫描零命中。

## Links

- Driven-By: `doc/issues/draft-2026-07-31-admin-console-api.md`（⑥-B；待拍板 #2 key 清单与文件格式、#4 写后生效语义；PR #31 评审 P2：key 白名单校验）
- 先例（⑥-A）：`doc/tasks/2026-07-31-admin-status.md`（PR #33 合入 origin/main；admin.go AdminHandler 注入模式、Pinger、writeError）
- 目标切片：`doc/tasks/2026-07-31-miniapp-v1-target.md`（⑥⑦）；⑦ 依赖本任务，但 ⑦ 仍挂起（discussions/21 等老板资源）
- 红线：`doc/PRIVACY.md`；契约：`doc/API.md`

## State

- **status**: `done`
- **last step**: dispatcher 建记录（2026-07-31 off-peak 排单；⑥-A 已合入，fix/admin-status-p2 评审中不影响本任务）

## Next

主会话：创建 worktree `.claude/worktrees/admin-config`（分支 `feat/admin-config`，base `origin/main` 最新）→ 派 coder 实现（config 包 + 端点 + 单测/契约测 → 本地 verify 绿 → push）→ reviewer 独立评审（PRIVACY 扫描、key 白名单、0600/原子写断言、泄漏断言覆盖）→ CI 绿 → 合入（与 ⑥-C 串行合入，主会话协调 main.go/API.md 冲突）→ 本记录置 done、落盘。
