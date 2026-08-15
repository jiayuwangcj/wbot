# 00700 日内 K 线回填:wbot ingest futu 子命令 + 30m/60m 数据落地(2026-08-15 老板指令)

## Goal

老板指令(2026-08-15):「先落地能拉到的0700精确数据准备日内级别的回测」+「先看能拉到什么时候的」。日内波动影响战术参数(move_interval_pct/stock_switch_pct)语义,回测升级日内粒度(30m/60m 决策点)的前置 = 正股日内 K 线落库。

落地:
- 新增 `wbot ingest futu` 子命令(复用 internal/futu.HistoryKline 分页拉取),支持分钟级 timeframe,幂等 upsert 进 bars 表
- 回填 HK.00700 30m + 60m K 线(**2023-01-01..2026-08-14**;老板 2026-08-15 追加指令:「先拉完2023年以后00700的数据并落地即可,之前的不要了」;深度实测 2015-04 起,2023 起完全可拉)

## 数据源实测(2026-08-15,网关 192.168.217.2:22222 /api/history-kline)

- **60m(K_9)深度:2015-04-16 起**(实测 begin=2000-01-01 返回最早 bar 2015-04-16 10:30);1000 根/页,next_req_key 翻页
- **30m(K_8)深度:同样 2015-04-16 起**(最早 2015-04-16 10:00)
- 回测窗口 2025-02-10..2026-08-14 完全覆盖,可拉全部
- HKEX 官方 Data Marketplace 为商业产品(无免费 API);Yahoo 拒连;腾讯/新浪港股分钟线实测不可用 → futu 网关是唯一可用源

## Constraints

- worktree: `.claude/worktrees/ingest-futu-kline`(分支 feat/ingest-futu-kline,基于 origin/main=9a56e1b)
- 复用 internal/futu:`HistoryKline`(分页 next_req_key + 限频已实现,kline.go:119)、`ParseTimeframe`(K_1M..K_MONTH)、`ParseAdjust`、`futu.Client{BaseURL: $FUTU_GATEWAY_URL}`
- 对齐现有 ingest 模式:`cmd/wbot/ingest_tencent.go`(flag 风格、-dry-run/-from/-to/-source/-dsn、`ingest.ValidateBars` → `ingest.RunIngestion(label, symbol, tf, adjust, source, from, to, prefetchedBars)` 幂等 upsert + ingestion_runs、`db.MigrateUp`、WBOT_PG_DSN)
- **adjust 默认 none(真实成交价)**:与期权快照底层价同口径,避开复权因子污染(日 K 的 tencent qfq 污染问题已排期另行处理);-adjust 可覆盖
- source 默认 "futu";timeframe 支持 K_30M/K_60M/K_15M/K_5M/K_1M/K_DAY 等(ParseTimeframe)
- 命令帮助文本写清深度/分页语义;单测用 httptest 假网关(仿 ingest_tencent_test)
- 提交前 scripts/verify.sh 全绿;真实 PG 回填后校验(密度/首末时间/与日K对照)
- 编码执行:codex(gpt-5.6-luna);提交署名 `Co-Authored-By: gpt-5.6-luna <noreply@openai.com>`;reviewer 评审后合入

## Links

- 前置:#66(腾讯日K回填)、#67(HKEX 期权日终)、#86(OTM 修复)、#87(全历史重训,日内升级的前置)
- 复用代码:internal/futu/kline.go、cmd/wbot/ingest_tencent.go、internal/ingest(RunIngestion/ValidateBars)
- 下游:日内粒度回测切片(引擎 30m 决策循环 + 战术参数日内语义),排期评估

## State

- [x] 数据源实测(30m/60m 深度 2015-04 起,可拉全部;1m/5m/15m 同样 2015-04-16 起)
- [x] 实施 `wbot ingest futu`(codex 额度尽 → Claude coder,9d08c63,verify 全绿)
- [x] 单测 + verify.sh 全绿(2026-08-15,verify: ok)
- [x] reviewer 评审(9d08c63 有条件合入:P1 补救文本补 -adjust fwd + P2 三处文档,5bc595c 修;0e0bd93 无条件合入)
- [x] **合入 main**:PR #349(0f4f67e,ingest futu 命令)+ PR #350(81d4f63,-skip-invalid 容错);分支/worktree 清理
- [x] **全量回填落地(2026-08-15,老板追加「能拉则拉」,窗口 2015-04-16..2026-08-14,adjust=none/source=futu,串行 + -skip-invalid)**:
  - 1m=918,380(跳过 6 根脏 bar)、5m=183,115(跳 5)、15m=61,035(跳 5)、30m=30,515(跳 5)、60m=16,648(跳 5)
  - 脏 bar:2016-07-25 早盘首根(high 186.9 < open 188,各粒度同源;二分定位曾误判 07-22,实际 07-25)
  - 密度校验:2015-04-16 起点/2023-06/2026-08-14 每交易日完整(1m 331 根);2016-07-25 各粒度精确缺 1 ✓;未触发网关限频
- [x] 任务记录收口(2026-08-15)

## Next

回填完成后:日内粒度回测切片(引擎 30m 决策循环 + 战术参数日内语义 + 期权快照日终映射),排期评估
