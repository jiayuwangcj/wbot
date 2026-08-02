# （草稿）GitHub Issue 正文 — Web watchlist 调参工作台（v4 阶段 A 切片 3）

**建议标题**：`[feature] Web watchlist 调参工作台：/ui/watchlist.html 增删标的 + 策略选择 + 参数表单`

---

## Goal（对齐 ⑫-c 非目标「Web UI 管理页面属 v4 后续切片」——现在就是后续；老板 2026-08-01「可调参」指令的体验端）

watchlist 调参目前**全在 CLI**（`wbot watchlist add -strategy covered-call -param strike_pct_otm=0.03` + JSON 参数），无图形界面。`/v1/watchlist` CRUD 与 `/v1/strategies`（模板+参数 schema）**已就绪**，本切片纯前端补上管理页：

1. `/ui/watchlist.html`：标的列表（symbol/strategy/params/updated_at）+ 增删入口。
2. 新增/编辑：symbol 输入 + 策略下拉（来自 `/v1/strategies`）+ **参数表单按 schema 动态渲染**（类型/默认值/范围校验，非法输入就地提示，不发出请求）。
3. 删除确认；操作结果与错误就地展示（复用 app.js 的 fetchJSON/showError 模式）。
4. 遵守 Web 前端既有约束：零外部资源（无 CDN）、无构建链（纯 HTML/CSS/JS）、多页静态站、移动断点预埋（⑨）。

### 验收（可测）

- 页面源码断言（webui 包测试，沿用现模式）：watchlist.html 含对 `/v1/watchlist`、`/v1/strategies` 的 fetch 引用；含 PUT/DELETE 调用；参数表单渲染逻辑（按 schema 字段生成 input）存在；全部 html/css/js 无 `http://`/`https://` 外部引用；含 viewport meta。
- httptest（serve 级）：`GET /ui/watchlist.html` → 200 + `text/html`；`/ui/` 静态资源回归（css/js 200）。
- 与 CLI 行为一致：同一组 API（`/v1/watchlist` CRUD + `/v1/strategies`），集成测沿用 httpapi 既有契约测（**后端零改动**）。
- `go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`；CI 绿。

### 非目标

- 回测执行/触发（S4）、结果展示（S2）、后端 API 变更（无）、配置值渲染（PRIVACY 红线，不请求 /v1/admin/config 值）。

### 依赖

- 无新后端依赖（⑫-c 已合入：/v1/watchlist、/v1/strategies）；与 S1（回测结果 API）并行，文件面零重叠（本切片只改 `internal/webui/web/`）。

## 仓库内链回

- 需求源：draft-2026-08-01-strategy-options.md ⑫-c（非目标：「watchlist 管理前端属 v4 Web 后续切片」）；老板 2026-08-01「可调参（watchlist/每标的参数）」；产品体验意见 2026-08-02 项 2
- 现状复用：`internal/webui/web/`（index.html/admin.html/app.js 模式）、`internal/httpapi/watchlist.go`（⑫-c）、`doc/API.md`（/v1/strategies、/v1/watchlist 章节）

## Plan（可勾选）

- [x] watchlist.html + app.js 工作台逻辑（列表/增删/策略下拉/参数表单动态渲染/就地校验）
- [x] 页面断言测试 + httptest 回归 + verify/CI 绿

## 状态（2026-08-03）

✅ **已完成并合入**：watchlist.html + app.js 工作台（列表/增删/策略
下拉/参数表单动态渲染/就地校验/表排序）均已落地。闭环记录见
`doc/tasks/2026-08-02-watchlist-*.md`。
