# cluster 端点补 options_freshness (S-options-cluster) — 2026-08-03

状态: ✅ 已合并 (PR #172, commit eadcbb6 + 155a286)

## 背景
AUTO_ADVANCE 根任务循环。draft-2026-08-02-data-freshness-monitor
非目标注记「期权区块暂只进 CLI 门禁,cluster 端点未扩展(需要时按
同一模式补)」——CLI 侧已收官(闭环 #22),本闭环按同一模式补
cluster 侧,数据新鲜度监控对期权在 Web 端也可见。

## 改动
1. **internal/httpapi/admin_cluster.go**:
   - `dataPlaneJSON` 新增 `OptionsFreshness []optionFreshnessJSON
     "options_freshness"`(独立字段,向后兼容)。
   - `optionFreshnessJSON{Underlying, Source, MaxTs,
     MaxTsAgeSeconds, Fresh}`——与 bars_coverage 同 staleness
     字段模式,判定用 `MaxAgeForOptions`(4h),与 CLI 期权区块
     同一规则。
   - `ClusterStore` 接口 + `fillPipelineAndDataPlane` 填充;DB down
     时为空数组(降级语义同 bars_coverage)。
2. **internal/httpapi/httpapi.go**:`Store` 接口 + `dbStore.
   OptionFreshness`(→ `ingest.QueryOptionFreshness`)。serveMux
   接线无需改(cluster handler 已用 store)。
3. **测试**:
   - `TestClusterOptionFreshnessFields`:2h → fresh / 100h → stale
     (4h 阈值),字段齐全。
   - `TestClusterComponents` 扩展 options_freshness 断言;
     `TestClusterQueryError` 加 options 分支;`TestClusterDBDown`
     断言 options_freshness 空数组。
   - webui `TestDataPageContract`:options-fresh-table/
     options-fresh-empty/renderOptionsFreshness/optionsFreshSorter
     断言。
4. **Web**:data.html「期权新鲜度」区块(独立表,5 列:
   标的/来源/最新/年龄/新鲜度);app.js `renderOptionsFreshness`
   + `OPTIONS_FRESH_SORT_KEYS` + `optionsFreshSorter`(默认 max_ts
   降序,与 coverage 一致);无 drill-in/补数据按钮(期权补数据走
   CLI `ingest futu-option`,模式不同)。
5. **文档**:API.md cluster 章节(options_freshness 字段表 +
   降级语义);DATA_PIPELINE.md 移除「未扩展」注记。
6. **scripts/accept-options-cluster.sh**(新增,运维沉淀):真实端点
   验收 2/2(seeded 2h→fresh/100h→stale + 数组向后兼容)。

## 验证
- `go test ./... -count=1` 全绿(19 包,含 PG 集成);`go vet` 干净
- dev-up smoke 10/10(新二进制自动重启)
- 实时端点:options_freshness 8 行(BTEXECOPT/HK.00700/STRAT* stale,
  ZZ* fresh/stale 与 CLI 输出一致),字段齐全
- 逐端点验收 2/2(scripts/accept-options-cluster.sh)
- CI: 首轮 test job 因 gofmt 对齐失败(手写 struct 字段对齐),
  fix commit 后 5/5 全 pass;PR #172 merged

## 备注
- **gofmt 教训**:CI 第一道 check 是 gofmt;手写 struct 字段
  对齐必须 `gofmt -w` 后本地再验(`gofmt -l .` 应为空)。
- **期权 Web 展示边界**:Data 页期权表只读展示,无 drill-in/补
  数据按钮;如需与 bars 同等的操作闭环,可后续给 options_freshness
  行加「拉取期权链」按钮(走 ingest futu-option),draft 未定。
