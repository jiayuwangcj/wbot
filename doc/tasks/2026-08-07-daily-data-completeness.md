# 排期:每日数据齐全检查独立模块(回测基石)

## 状态

**✅ 已完成**（2026-08-09，本地分支 `codex/feat-daily-data-completeness`；实现、独立审查与验收均完成）。

## 来源

老板指令(2026-08-07,原文):「进程应该每日检查当日关注的标的的所有可拉取数据是否齐全,这是后续回测的基石,独立模块保证」。

背景:2026-08-07 期权巡检发现 HK.00700 option_quotes 本周 0 行(最后落地 2026-07-31),根因=无内置定时拉取(依赖外部 cron)。本需求即该问题的系统化方案。

## 需求拆解

- **进程**:内置定时任务(serve 内 ticker,不依赖外部 cron;另需 CLI 子命令手动触发)
- **每日**:交易日收盘后定时跑(如本地 17:30,避开盘中数据未定型窗口)
- **当日关注的标的**:watchlist 实例列表(观察列表)
- **所有可拉取数据**:bars(8 周期 × 3 复权)+ 期权链(+ 账户快照?待定)
- **是否齐全**:完整性检查——每标的最短缺失(最近一个交易日的日线 bar 是否落地、期权最新 expiry 链是否落地、分钟线最近交易时段)
- **独立模块**:internal 新 package(如 `internal/datacheck`),不混入 httpapi/ingest

## 设计草案(实施时细化)

1. `internal/datacheck`:Check(ctx, watchlist) → per-symbol 完整性报告(齐全/缺失清单)
2. 缺失 → 自动经网关补拉(复用 ingest 流程,幂等);网关不可达 → 标记待补,下一轮重试
3. 触发:serve 内置每日 ticker + CLI `wbot datacheck [--now]` 手动触发 + API 只读视图?
4. 结果落库或日志 + 报告面(数据页「数据齐全」区块?)

## 验收

- [x] 独立模块 `internal/datacheck`；ingest/httpapi 既有接口不变
- [x] serve 每日 ticker 可配置（默认本地 17:30；`-datacheck-at` / `-datacheck-disable`）
- [x] 模拟缺失场景：逐项报告 missing/stale，repair 后复查转 complete
- [x] E2E：watchlist + bars + option_quotes 真 PostgreSQL；CLI 缺失报告 exit 1
- [x] `scripts/verify.sh` 全绿（test/vet/race/staticcheck/五平台构建/smoke/accept）

## 实施

- 完整矩阵：8 timeframe × 3 adjust + 每标的最新未到期期权链；账户快照仍按原需求保持在本轮范围外。
- `wbot datacheck` 默认只读，支持 `-json`；显式 `-repair` 复用 serve 的富途补拉器，单项失败继续、最后统一复查。
- bars 补拉窗口：普通周期 14 天、周线 60 天、月线 180 天；期权 7 天/最近一个到期日；继续复用 ingest 事务、限频、校验与幂等。
- 分钟/日线与期权按标的市场最新应有工作日判定（HK/沪深/US 收盘缓冲 + 周末）；周/月线沿用既有 timeframe 阈值。当前不内置交易所节假日日历，节假日最多产生一次无害补拉尝试。
- 后续 P1（2026-08-09）已增加只读 `GET /v1/datacheck` 与 Data 页摘要/缺失列表；repair 写面仍只在显式 CLI 与 serve scheduler，不暴露 HTTP。

## 验证记录

- `go test ./... -count=1`、`go vet ./...`：全绿。
- `scripts/verify.sh`：`verify: ok`（当前机器需临时把 Go bin 加入 PATH 以发现已安装的 staticcheck；未修改仓库配置）。
- 真 PG：因当前执行环境到 OrbStack 容器网络受限，将两个 CGO-disabled 测试二进制放入临时 `postgres:16-alpine` 容器内执行；`TestSnapshotIntegration` 与 `TestDataCheckCLIIntegrationReportsWatchlistGaps` 均 PASS，测试容器及启动的开发 PG 已恢复/清理。
- Luna 独立审查修复 option coverage 聚合误判：`MaxTs` 与 `MaxExpiry` 现在强制来自同一最新快照；历史更晚 expiry + 最新较早 expiry 的真实 PG 回归测试 PASS。

## 相关

- `doc/DATA_PIPELINE.md`(现外部 cron 依赖说明需更新)
- `doc/tasks/2026-08-07-lightweight-charts.md`(同一轮次)
