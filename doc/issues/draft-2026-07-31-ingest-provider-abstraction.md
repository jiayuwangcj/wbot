# （草稿）GitHub Issue 正文 — 数据源 Provider 抽象

**建议标题**：`[feature] ingest 数据源 Provider 抽象：可配置、可插拔、可 mock`

---

## Trigger comment

- 仓库级锚点：<https://github.com/jiayuwangcj/wbot/issues/8#issuecomment-4268661869>
- 若单独开 Issue：发帖后在同一 Issue 留锚点评论，把评论 URL 填回此处并作为 PR 的 `Driven-By:`。

## Goal（对齐 [[ROADMAP]] v1「数据源与券商侧抽象，单元测试可 mock，集成测再接真实凭证」）

现有 `internal/ingest.Source` 接口（`Bars(ctx, symbol, timeframe, from, to)`）已抽象「怎么取 bars」，但**来源的创建与配置仍硬编码在 CLI**（`runIngestMock`/`runIngestFile`/`runIngestURL` 各自直接实例化）。接真实行情源（Futu/IBKR 数据 API）前，需要一层 **Provider**：按名称注册、按配置构造 Source、认证信息走环境变量。

### 范围（建议的最小拆法）

1. `internal/ingest/provider.go`：`Provider` 概念——`type Provider struct { Name string; New func(Config) (Source, error) }` + 全局注册表 `Register(name, Provider)` / `NewProvider(name)`；内建注册 `mock` / `file` / `url` 三个（行为与现状一致）。
2. CLI：`wbot ingest` 增加 `-provider <name>`（默认按子命令推断：`ingest mock`→mock、`ingest file`→file、`ingest url`→url），为未来 `ingest provider` 统一入口铺路；或直接新增 `wbot ingest -provider url -url ...` 风格（设计取舍见 Issue 讨论）。
3. 配置承载：`Config` 用简单 map/struct 透传（URL、Path 等），敏感项（token）只从环境变量读，不入 `ingestion_runs`。
4. 测试：注册表单测（注册/查重/未注册报错）、三个内建 Provider 构造 + 既有 Source 测试回归。

### 验收

- `go test ./... -count=1`、`go vet ./...`、`scripts/verify.sh` 绿；CI（test + db-integration）绿。
- 既有 `wbot ingest mock|file|url` 行为与输出不变（向后兼容）。
- 未注册的 provider 名 → CLI 报错退出 2。

### 非目标

- 真实行情源接入（待具体券商/数据源凭证后另开 Issue）。
- 券商侧下单抽象（ROADMAP v3/v2 范围）。
- schema 变更、Redis。

## Plan（可勾选）

- [x] `provider.go` 注册表 + `mock/file/url` 内建注册与单测
- [x] CLI `-provider` 接线（保持旧命令行为）+ main_test 用例（未注册名 → 2）
- [x] 文档：`doc/DATA_PIPELINE.md` 补 Provider 一节

## 仓库内链回

- 设计背景：`doc/DATA_PIPELINE.md`、`internal/ingest/`（bar.go/run.go/file.go/http.go/mock.go）
- 任务轨迹：`doc/tasks/2026-07-31-ingest-time-range.md`（前置已完成）

## 状态（2026-08-03）

✅ **已完成并合入**：`internal/ingest/provider.go` 注册表 +
`mock/file/url` 内建注册、CLI `-provider`（未注册名退出 2）、
`doc/DATA_PIPELINE.md`「Provider 抽象」一节均已落地。真实行情源
接入仍为非目标（待凭证）。
