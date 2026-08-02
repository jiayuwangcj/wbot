# （草稿）GitHub Issue 正文 — 数据新鲜度监控：staleness 判定 + CLI 检查 + admin 页展示

**建议标题**：`[feature] 数据新鲜度监控：`wbot ingest freshness` 检查（exit 门禁）+ /v1/admin/cluster 新鲜字段 + admin 页标注`

---

## Goal（对齐 [[ROADMAP]] v1「数据正确性优先」——拉取、校验、落地已有，但**新鲜度无人观察**）

`-every` 定时拉取已在跑（`RunEveryResilient`，失败容忍），`/v1/admin/cluster` 已返回 bars_coverage（per symbol×timeframe 的 max_ts），但**没有 staleness 判定**：数据停更几天无人知晓，回测基于过期数据空转。本切片补齐「数据新鲜度」可观测闭环，且 CLI 检查可接 cron 做脚本化告警门禁。

### 范围（建议的最小拆法：3 个子切片）

#### 子切片 A：staleness 判定 + CLI `wbot ingest freshness`

- `internal/ingest` 新增 freshness 查询与判定：per symbol×timeframe×adjust×source（bars 表 PK 四元组）返回 `max_ts` 与 age；判定三态 **fresh / stale / unknown**（无数据）。
- 阈值语义（待拍板，建议默认）：timeframe 映射期望窗口（如 `1d` → max_ts 距今 ≤ 3 天为 fresh；`-max-age` 参数可覆盖全局，单位小时）；时钟以 now() 为准。
- CLI：`wbot ingest freshness [-dsn] [-max-age]` 输出每组合 `symbol timeframe max_ts age status`；**任一 stale → exit 1**（脚本化门禁，可入 cron：`ingest freshness || 告警`）；无数据 → unknown 输出 + exit 0（或可加 `-strict` 使 unknown 也 fail——待拍板）。
- 与既有 `ingest status`（最近 runs）互补：status 看「拉取任务成败」，freshness 看「数据是否新鲜」。

#### 子切片 B：API 扩展 `/v1/admin/cluster`

- bars_coverage 每项增 `max_ts_age_seconds` 与 `fresh` 字段（**向后兼容**：老字段不变，新增字段缺省时客户端不渲染）——零新端点；或独立 `/v1/admin/freshness`（待拍板，产品建议前者）。

#### 子切片 C：admin.html 数据面板标注

- admin.html 数据面板（bars_coverage 表格）对 stale 行标「数据过期」+ 样式；unknown 行标「无数据」。

### 验收（可测）

- 单测（判定函数）：fresh/stale/unknown 三态、阈值边界（等于阈值算 fresh 或 stale——定义并断言）、空表 unknown、`-max-age` 覆盖。
- CLI `main_test`：`wbot ingest freshness` 在 stale 数据上 exit 1、fresh 数据 exit 0、参数错误 exit 2。
- 集成测（有 `WBOT_PG_DSN` 真 PG，无则 skip）：`wbot ingest mock` 后 freshness 全 fresh；`-max-age 1h` 后 stale → exit 1。
- httptest：cluster 响应含新字段（`max_ts_age_seconds`/`fresh`）且旧字段值不变（向后兼容断言）。
- admin.html 页面断言：含 stale 渲染逻辑引用。
- `go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` → `verify: ok`；CI（test + db-integration）绿；`doc/API.md` cluster 章节更新。

### 非目标

- **告警推送**（通知通道属产品体验意见 9，blocked——待老板 token；本切片只做 exit 门禁与页面标注，cron 侧可自行接 `ingest freshness || notify`）。
- 自动补拉/修复（人工或既有 `-every` 调度即可；自动修复属后续切片）。
- `option_quotes` 表新鲜度（期权数据侧可后续并入同一判定）。
- 历史新鲜度趋势图（后续）。

### 依赖

- **无外部依赖**（纯 DB 查询 + 既有端点扩展）。
- 前置复用：`internal/httpapi/admin_cluster.go`（BarCoverage/max_ts 已有）、`internal/ingest`（bars PK 四元组、`ingest status` 模式）、`internal/webui/web/admin.html`。

## 仓库内链回

- 路线：[[ROADMAP]] v1 数据管道（可观测闭环；「数据正确性最高优先」）；`doc/tasks/2026-07-31-ingest-every-resilient.md`（-every 失败容忍已做，Next 提到「bars 完整性校验」方向）
- 现状：`internal/httpapi/admin_cluster.go`（dataPlaneJSON.BarsCoverage 含 min/max_ts）、`internal/db` BarCoverage、`internal/ingest/status.go`（RecentRuns）
- 需求源：产品组自主任务生成（2026-08-02，PM 队列扫描为空授权）；产品体验意见 2026-08-02 项 9 通知的前置（告警源即本切片 stale 判定）

## Plan（可勾选）

- [x] A：freshness 判定 + `ingest freshness` CLI + 单测/集成测
- [x] B：cluster bars_coverage 新字段 + 向后兼容断言 + API.md
- [x] C：admin.html stale/unknown 标注 + 页面断言 + verify/CI 绿

## 状态（2026-08-02）

✅ **已完成并合入**：PR #88（主体三切片）+ #90（review P3：负 `-max-age`
拒绝）。闭环记录见 `doc/tasks/2026-08-02-data-freshness-monitor.md`
（补归档）。非目标项（告警推送/自动补拉/option_quotes 并入/趋势图）
仍开放。
