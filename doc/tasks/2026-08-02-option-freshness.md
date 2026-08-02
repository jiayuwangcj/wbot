# option_quotes 新鲜度并入 freshness 判定 (S-option-freshness) — 2026-08-02

状态: ✅ 已合并 (PR #169, commit 346e656)

## 背景
AUTO_ADVANCE 根任务循环。`ingest freshness` 只判定 bars 表
(symbol×timeframe),期权行情(option_quotes)不在门禁内——数据停更
只能靠 status 侧观察。本闭环取 draft-2026-08-02-data-freshness-monitor
的「期权数据侧可后续并入同一判定」非目标项,按同一模式补齐。

## 改动
1. **internal/ingest/freshness.go**:
   - `MaxAgeForOptions = 4h`(期权日内行情,与 bars 的 3×nominal 逻辑
     无关;4h 覆盖周末休市,工作日单日 6.5h 交易 → 隔夜 16h 仍 stale
     的设计留待后续按需调)。
   - `OptionFreshness{Underlying, Source, MaxTs, AgeSeconds}` +
     `QueryOptionFreshness`:GROUP BY underlying, source 取 max_ts。
2. **cmd/wbot/main.go** `ingest freshness`:bars 区块后追加期权区块
   (same 三态 + `-max-age` 全局覆盖),`anyStale` 合并;usage 文本更新。
3. **测试**:
   - 单元:`TestMaxAgeForOptions`(4h)、`TestQueryOptionFreshness_validation`。
   - 集成 `TestQueryOptionFreshnessIntegration`(ZZOPTFRESH/ZZOPTSTALE,
     2h → fresh、100h → stale、1000000h 覆盖翻转)。
   - CLI 集成 `TestIngestFreshnessIntegration`:期权区块插入
     ZZOPTCLIFRESH/ZZOPTCLISTALE,默认 exit 1 + stale 行,-max-age
     覆盖 exit 0 + 全 fresh。
4. **scripts/accept-option-freshness.sh**(新增,运维沉淀):真实 CLI
   exit 码验收——默认阈值 stale 期权 → exit 1、-max-age 1000000h →
   exit 0 且 stale 行翻 fresh;ACCOPT* 前缀数据自清理。
5. **doc/DATA_PIPELINE.md**:freshness 章节期权区块、4h 阈值、退出码
   范围说明。

## 验证
- `go test ./... -count=1` 全绿(19 包,含 PG 集成)
- dev-up smoke 10/10(二进制变化自动重启)
- 逐端点验收 6/6(真实 CLI:exit 1 门禁 / fresh+stale 行渲染 /
  stale 提示 / -max-age 翻转 ×2),脚本已沉淀 repo
- CI: 5/5 全 pass;PR #169 merged

## 备注
- **期权区块暂只进 CLI 门禁**:`/v1/admin/cluster` 的 coverage 未扩展
  (bars 才带 max_ts_age_seconds/fresh 字段);需要时按同一模式补
  option 字段(向后兼容),draft 中已注明。
- **cluster 端与 CLI 端同阈值规则**:MaxAgeForTimeframe 共用,期权
  若入 cluster 用 MaxAgeForOptions,两边默认一致。
