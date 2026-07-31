# Web 前端 ⑧⑨：PC Web（go:embed 静态 UI）+ 移动响应式预留

- **id**: `2026-07-31-web-frontend`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

目标切片 ⑧（PC 端 Web 前端，第一步）+ ⑨（移动版本 Web 框架预留）：「`wbot serve` 同时提供静态 Web UI」——go:embed 静态资源构建进二进制，根路径 `/` 提供入口页面、`/ui/` 提供静态资源与页面；页面覆盖**数据展示（bars/runs 查询）**与**后台管理（status/config/cluster 状态页，读 API）**；布局层预留移动响应式（viewport/断点），PC 页面可复用。

## Constraints

1. **mux 共存设计决策（必读，runServe 现状）**：`cmd/wbot/main.go` 的 runServe top mux 现注册：`/v1/admin/`（AdminHandler）、`/v1/admin/cluster`（exact）、`/v1/admin/config` + `/v1/admin/config/`（ConfigHandler，config.OpenDefault 成功才注册）、`/` → `httpapi.Handler(store)`（内部 mux：/v1/bars、/v1/runs、/v1/health，`/` catch-all → JSON 404）。**不可把 `/` 的注册换成 FileServer**——/v1/bars 等 API 路径依赖 `/` catch-all 分发，替换即吞掉 API。**决策（落档）**：利用 Go 1.22 ServeMux（go.mod `go 1.22.0`）精确根匹配新增 `top.Handle("GET /{$}", http.RedirectHandler("/ui/", http.StatusMovedPermanently))`（`/` → 301 `/ui/`），新增 `top.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(subFS))))`；最长匹配优先，与既有 `/` catch-all 零冲突。行为变化落档：`GET /` 由 JSON 404 变为 301（预期，产品目标）；其余未知路径仍 JSON 404。index.html 唯一来源在 `/ui/`，根路径仅入口重定向。
2. **静态资产放新包 `internal/webui/`**（`//go:embed web/*` 子目录，如 `web/index.html`、`web/assets/`），main.go 只做注册；**不引入构建链**：纯手写 HTML/CSS/JS，无模板引擎/前端框架/图表库。
3. **离线可用（无 CDN）**：页面零外部 URL 引用（无 CDN 字体/JS/CSS）——验收含源码扫描断言。
4. **后台管理页为读 API 页面**：status/cluster/config 只读展示（config 只渲染 `set`/`updated_at` 元数据）；PUT 配置不在本任务。PRIVACY 红线：页面不请求、不渲染任何配置值。
5. **鉴权沿用现状**：默认 127.0.0.1 绑定，不加登录/token（与 ⑥ 后端决策一致）。
6. 页面形态：**多页静态站**（如 index.html 数据页、admin.html 管理页），无 SPA 路由/无 history API。
7. 日志沿用 `httpapi:` 前缀风格；`serve -h` 帮助文本补 Web UI 说明 + `main_test` 断言。
8. **冲突面提示**：本任务改 `cmd/wbot/main.go`（runServe mux 注册区 + -h 文本）与 `main_test.go`；`internal/httpapi/` **零改动**。与其它进行中任务无重叠文件（⑥ 系列已全部合入 origin/main）。
9. 不新增 Go 依赖（标准库 + embed）。

## 验收（可测）

- **httptest（serve 级）**：
  - `GET /` → 301 且 `Location: /ui/`；
  - `GET /ui/` → 200 + `text/html` + 含关键元素（`<title>`/页面标题）；
  - `GET /ui/` 下静态资源（css/js）→ 200 + 正确 content-type；`GET /ui/` 不存在文件 → 404；
  - **API 回归**（证明 `/` catch-all 未被破坏）：`GET /v1/bars` 缺参 → 400、`GET /v1/runs` → 200、`GET /v1/health` → 200（真 PG 时）/mock 断言、未知 `/v1/xxx` → JSON 404。
- **页面源码扫描**（webui 包测试）：全部 html/css/js 无 `http://`/`https://` 外部资源引用；index.html 含 viewport meta（⑨）；嵌入 CSS 含 media query 断点（⑨，如 ≥1024px 多列 / <768px 单列）；页面含对 `/v1/bars`、`/v1/runs`、`/v1/admin/*` 的 fetch 引用（②③ 骨架断言）。
- `go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`；CI **test** job 绿（本变更含 go 文件，非 docs-only，CI 必然可达）；无新 DB 依赖，db-integration 不受影响。
- `go build ./cmd/wbot` 产物含静态资源（go:embed 构建进二进制；httptest + binary smoke 覆盖）。

## 拆解（子切片，串行派单）

| 子切片 | 内容 | 验收核心 |
| --- | --- | --- |
| ① serve 接入静态骨架 | `internal/webui` embed FS + `/ui/` FileServer + `GET /{$}` 根入口 + 占位 index.html；serve -h 更新 | httptest：`/` 301、`/ui/` 200、API 回归全绿 |
| ② 数据页 | index.html 查 `/v1/bars`（symbol/timeframe 输入）+ `/v1/runs`（最近运行）表格渲染（纯 JS fetch） | 页面含对应 fetch 引用；零外部资源 |
| ③ 管理页 | admin.html 读 `/v1/admin/status`、`/v1/admin/cluster`、`/v1/admin/config` 只读展示（config 仅元数据） | 同上 + 无配置值渲染断言 |
| ④ 响应式预留 | viewport + 断点 media query 布局（PC 多列 / 移动单列），②③ 页面复用 | viewport meta + media query 断言 |

## Links

- Driven-By: `doc/tasks/2026-07-31-web-v1-target.md`（切片⑧⑨；superseded `2026-07-31-miniapp-v1-target`）
- 路线：`doc/ROADMAP.md` v4（go:embed Web UI）；契约：`doc/API.md`（端点清单）；红线：`doc/PRIVACY.md`
- 先例：`doc/tasks/2026-07-31-admin-status.md` / `-admin-config.md` / `-admin-cluster.md`（done：端点模式、runServe mux、verify 验收链）；`-httpapi-serve.md`（serve 骨架）
- 现状：`cmd/wbot/main.go` runServe（mux 注册顺序）、`go.mod`（go 1.22.0）、`.github/workflows/ci.yml`（test job）

## State

- **status**: `done`
- **last step**: dispatcher 建记录（2026-07-31 off-peak）。切片⑧⑨ 排定：⑧ 依赖 ① 已满足（/v1/bars、/v1/runs、/v1/admin/* 均在 origin/main）；⑨ 随 ④ 随 ②③ 布局预留，不单独阻塞。本轮只派子切片①（今天已连续多轮，拆小步）。

## Next

主会话：创建 worktree `.claude/worktrees/web-frontend`（分支 `feat/web-frontend`，base `origin/main` 最新）→ 派 coder 实现**子切片①**（embed + `/ui/` + 根入口 + httptest 回归 + serve -h）→ 本地 verify 绿 → push → reviewer 独立评审（重点：mux 共存不破坏 API 404 语义、零外部资源离线可用、无配置值）→ CI 绿 → 合入 → 本记录更新状态、随后派 ② 数据页 → ③ 管理页 → ④ 响应式。
