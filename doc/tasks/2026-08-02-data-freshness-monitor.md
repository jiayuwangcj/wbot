# 数据新鲜度监控:staleness 三态判定 + CLI 门禁 + cluster 字段 + admin 标注 (S-freshness) — 2026-08-01(补归档 2026-08-02)

状态: ✅ 已合并 (PR #88 + #90, 2026-08-01;commit 4451767 / 389e7cf / f8349e5)

> 补归档说明:本闭环功能 2026-08-01 已实现并合入,但未落盘闭环记录,
> 2026-08-02 AUTO_ADVANCE 巡检发现违反「运维沉淀」规则,补记。
> 设计文档:doc/issues/draft-2026-08-02-data-freshness-monitor.md(已标 ✅)。

## 背景
产品组自主任务(draft 2026-08-02,PM 队列扫描为空授权):`-every` 定时
拉取已在跑(失败容忍),`/v1/admin/cluster` 已返回 bars_coverage,但
**没有 staleness 判定**——数据停更几天无人知晓,回测基于过期数据空转。
本闭环补齐「数据新鲜度」可观测闭环,CLI 检查可接 cron 做脚本化告警门禁。

## 改动(三切片,PR #88 主体 + #90 review P3 修复)

### 子切片 A:staleness 判定 + CLI `wbot ingest freshness`
- `internal/ingest/freshness.go`:`FreshnessStatus` 三态 fresh /
  stale / unknown;`JudgeFreshness`(无数据→unknown,age ≤ maxAge→
  fresh,边界含等号;未来 ts 按 0 秒);`QueryFreshness` 遍历
  bars_coverage 计算 age;timeframe→期望窗口映射(1d→72h 等,
  无法解析回落 24h)。
- CLI `wbot ingest freshness [-dsn] [-max-age]`:`-max-age` 全局覆盖
  阈值(小时);任一 stale → exit 1(脚本化门禁,可入 cron:
  `ingest freshness || 告警`);unknown 输出但不 fail。
- #90(review P3):负 `-max-age` 拒绝。

### 子切片 B:API 扩展 `/v1/admin/cluster`
- bars_coverage 每项增 `max_ts_age_seconds` 与 `fresh` 字段——
  **向后兼容**:旧字段不变,老客户端忽略新字段即可(零新端点)。

### 子切片 C:admin.html 数据面板标注
- app.js `freshnessCell`:stale 行「数据过期」+ 红色样式,unknown 行
  「无数据」;cluster 卡片统计 stale/unknown 数量 + 徽章。

### 测试(草稿验收清单逐项落实)
- freshness 单测:三态、阈值边界(等于阈值算 fresh)、空表 unknown、
  未来 ts、`-max-age` 覆盖(ingest/freshness_test.go)。
- CLI main_test:help/坏 flag exit 2、无 DSN exit 2、集成测(stale 上
  exit 1、fresh 上 exit 0)。
- httpapi TestClusterFreshnessFields:新字段存在 + 旧字段值不变
  (向后兼容断言)。
- webui:stale 行渲染逻辑引用(数据过期/freshnessCell)。
- doc/API.md cluster 章节更新。

## 验收
- `go test ./... -count=1` 全绿;`go vet` 绿;CI(test +
  db-integration)绿;PR #88/#90 merged。
- 现场复核(2026-08-02 补归档时):`wbot ingest freshness` 实跑
  FRESH.US fresh、历史测试数据 stale,输出与 exit 门禁正常。

## 备注
- **非目标**(草稿明确,后续可做):告警推送(等推送 token,接
  `ingest freshness || notify` 即可)、自动补拉/修复、option_quotes
  表新鲜度并入同一判定、历史新鲜度趋势图。
- **阈值语义**:timeframe 默认窗口映射(1d→72h),`-max-age` 全局
  覆盖;等于阈值算 fresh(边界含等号)。
- **与 `ingest status` 互补**:status 看「拉取任务成败」,freshness
  看「数据是否新鲜」。
