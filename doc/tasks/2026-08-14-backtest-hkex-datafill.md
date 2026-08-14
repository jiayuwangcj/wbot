# HKEX 港股期权日终数据回填(2026-08-14,老板指令:搜索其他数据源)

## Goal

接入 HKEX 官方免费港股期权日终数据：DTOP settlement/成交/OI + RP006 IV/标的 settlement。实现 HK.00700 历史回填，解决「期权 snapshot 仅少量实时日、回测无成交尝试」的数据瓶颈，同时保持实时提醒与事件级回测的能力边界。

## 已知线索(主会话 2026-08-14 实测/搜索)

- **DTOP**(Daily Trading Activity & Open Positions Summary):每日所有股票期权类成交/未平仓/结算价,zip 命名 `DTOP_O_yyyymmdd.zip`
  - 页面:https://www.hkex.com.hk/eng/stat/dmstat/oi/oi_o.asp
  - 直链:https://www.hkex.com.hk/eng/stat/dmstat/oi/DTOP_O_20250702.zip
- **Series Prices Raw Data File**(系列价格原始数据):https://www.hkex.com.hk/eng/market/rm/rm_dcrm/riskdata/srprices/seriesprices.asp(月度归档 seriesprices.asp?YYYYMM=)
  - 直链:https://www.hkex.com.hk/eng/market/rm/rm_dcrm/riskdata/srprices/RP006_250702.zip
- **Data Download Centre**:https://www.hkex.com.hk/eng/stat/dmstat/datadownload/setdata.asp?YYYYMM=202608(月度页面,无直接 zip 链接)
- 免费(官网直接下载);深度 tick 数据是 HKEX Data Marketplace 付费服务(不用)

## 任务清单

1. **链路勘察**:定位 DTOP/Series Prices 真实下载 URL(抓包页面 JS/表单,或 HKEX 已知命名模式变体如 oi_o_YYYYMM.zip / DTOP_O_YYYYMMDD.zip 各变体),实测下载一个日文件并解析字段(TCH: 合约代码/结算价/成交/未平仓)
2. **回填工具**:`wbot ingest hkex`(或扩展 ingest tencent)拉取 HKEX 日终 → 写入期权 quotes 表(幂等:按 symbol+contract+ts 去重;结算价映射 option price 字段,source=hkex 标注)
   - 目标:HK.00700 期权历史 ≥300 个交易日(可先拉最近 1-2 年,按日拉取限频)
3. **回测消费**:回测引擎确认能消费 hkex 源的期权 quotes(与 futu snapshot 并列/优先顺序文档化);HK.00700 报告从「无成交尝试」变为有真实成交模拟
4. **验收**:真实 PG 回填后 HK.00700 期权 bars 数量显著提升;回测报告 net_return 非零且成交记录存在(如数据窗含成交);accept 脚本挂 CI

## Constraints

- worktree: `.claude/worktrees/backtest-hkex`(分支 fix/backtest-hkex,基于主基线)
- 提交前 scripts/verify.sh 全绿;署名按实际编写模型
- HKEX 免费站点限频礼貌(拉取间隔 ≥1s,失败退避);标注 source=hkex
- 回填不碰实时链路;真实 PG 验收

## Links

- 搜索/实测日志:主会话 2026-08-14(HKEX Data Download Centre / DTOP 含 Settlement Price)
- 数据面裁决:doc/tasks/2026-08-14-backtest-p0-sol.md(P0-1 futu 历史不可拉)、doc/issues/2026-08-14-tencent-hk-option-api-investigation.md(腾讯不可用)
- 美股结论:Yahoo OCC 实测被反爬拦截;Eulerpool free tier(10 年历史)可评估——本轮不做,美股维持每日积累

## State

- [x] HKEX 下载链路定位 + 日终文件解析
- [x] ingest hkex 幂等回填(00700 期权历史)
- [x] 回测消费 + 真实 PG 验收(报告有真实成交)
- [x] accept 脚本挂 CI

## 实现与证据（2026-08-14）

- 真实下载 `DTOP_O_20250702.zip` 与 `RP006_250702.zip`。DTOP SEOCH all raw 的 `TCH / 30 JUL 25 / 500.00`：Call gross OI `10228`、成交 `2649`、settlement `12.40`；Put gross OI `17131`、成交 `3067`、settlement `9.82`。RP006-FINAL 的 `TCH500.00G5/S5` settlement 分别同为 `12.40/9.82`，IV 为 `20.5604%/19.4420%`，`TCHSP` 标的 settlement 为 `501.50`。解析器按业务日期和系列 settlement 交叉校验。
- `wbot ingest hkex` 默认 `HK.00700/TCH/100`，官方地址请求起点间隔 ≥1 秒并对网络、429、5xx 做退避。每个交易日一个事务；`option_quotes` 写 `source=hkex,adjust=none,OHLC=settlement`，snapshot 使用 `hkex-eod-YYYYMMDD-bs-r0` 唯一键，原区间重跑零重复。
- 为现有 bar-time Wheel runner 写入明确的日终研究投影：`bid=ask=settlement`，IV/成交/OI 来自官方文件，Delta/Theta 由官方 IV/标的 settlement 按 Black-Scholes `r=0` 派生。只有同一 HKEX 合约覆盖 DTE ≥10 到 ≤1 且有可用 bar 才输出 `RESEARCH_ONLY` 非空机械收益；实时 Wheel/LLM/Telegram 与事件级能力不解锁。
- 真实历史边界已纳入：DTOP 休市包可能是 `no_trading_activities.txt` 或只有 header/trailer；2025-07-14 的 RP006 官方包只有 `rp006.txt = "No File Available Yet"`，该日保留 DTOP settlement、计入 `quote_only_days`，不伪造 IV/Greeks；后续 RP006 恢复时只补原 null IV。
- `scripts/accept-hkex-datafill.sh` 已接 CI `db-integration`，真实 PostgreSQL 10/10：真实形状 ZIP → 24 settlement 行（含 1 个 RP006 缺口日）/22 研究 snapshot、重复回填零新增、完整周期、Wheel 模拟成交、`RESEARCH_ONLY` 非空收益。
- 真实 HK.00700 全窗 `2025-02-10..2026-08-13`：372 个交易日，`option_quotes=314361`（7550 个合约，291182 行带官方 IV），`option_quote_snapshots=120858`/371 日（全部通过 bid=ask、Greeks、IV、volume/OI、lot=100 完整性查询）；唯一少一天即 2025-07-14 RP006 官方缺档。真实 2025-07-02 单日复跑解析 672/246 行、`inserted_quotes=0,inserted_snapshots=0`。
- 全窗真实 PG Wheel 验收（初始资金 1,000,000、每笔费用 3）：372 bars、258 ready bars、完整周期 true、186 次尝试/140 次模拟成交、费用 420、`net_return_pct=0.047234`、`net_return_amount=47234`；报告 `bt-HK.00700-42-411c04e2` 为 `RESEARCH_ONLY/research_only`，风险卡明确非可执行 bid/ask。
- 提交门禁：`scripts/verify.sh` 全绿（frontend/npm audit、gofmt、`go test ./...`、vet、race、staticcheck、五目标交叉编译、CLI smoke、自包含 accept），最终输出 `verify: ok`。

## Next

独立 reviewer 判定 feature/发现 → 主会话决定合入。
