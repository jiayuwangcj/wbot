# 回测历史数据回填:腾讯免费日 K(2026-08-14 老板指令:找腾讯数据库)

## Goal

futu 无法拉取历史期权行情(P0-1 裁决),老板指令改用腾讯免费数据源。实测结论(2026-08-14 主会话验证):

- **HK.00700 日 K 可用**:`https://web.ifzq.gtimg.cn/appstock/app/fqkline/get?param=hk00700,day,,,1000,qfq` 返回 1000+ 条(2022-07-22 至今,开/收/高/低/量),免费
- **US.JD 不可用**:腾讯美股(含 usfqkline 专用接口)仅返回最近 1 个交易日

目标:实现腾讯日 K 回填工具,将 00700 历史日 K 回填 bars 表,提升回测数据覆盖率;JD 如实标记继续靠每日积累;调查腾讯港股期权历史接口可用性。

## 任务清单

1. **回填工具**:`wbot ingest tencent`(或独立子命令)拉取腾讯日 K(标的历史)→ 写入 bars 表(幂等:按 symbol+ts 去重,重复跑不产生重复 bar)
   - HK.00700:全量回填(可拉到 4 年,1000+ 条;先拉 320 条/1000 条按需)
   - US.JD:拉取后仅当日 1 条,如实入库(不伪造历史);输出「腾讯美股仅当日,历史靠积累」提示
   - 数据字段:Ts/Open/High/Low/Close/Volume;qfq 前复权语义标注(报告数据质量卡标注 source=tencent,adjusted=qfq)
2. **港股期权历史调查**:腾讯港股期权接口(qt.gtimg.cn 期权代码格式/历史 K)可用性;结论如实记录(可用→实现回填;不可用→记录,期权面继续靠 snapshot 积累)
3. **数据质量卡标注**:bars 数据源与复权方式进报告 data_quality(区分 futu 实时 snapshot vs tencent 回填)

## Constraints

- worktree: `.claude/worktrees/backtest-datafill`(分支 fix/backtest-datafill,基于 #65 合入后主基线)
- 提交前 scripts/verify.sh 全绿;署名按实际编写模型
- 腾讯接口免费额度无鉴权,限频(每次拉取间隔 ≥1s,失败指数退避),标注数据源
- 回填不碰实时链路(wheelrun 仍在用 futu 实时 snapshot);回填数据只服务回测
- 数据库写入幂等;真实 PG 回填后验收:00700 bars 覆盖显著提升(目标 ≥300 交易日)

## Links

- 实测日志:主会话 2026-08-14 腾讯接口验证(00700 1000+ 条 / JD 1 条)
- 数据面裁决:doc/tasks/2026-08-14-backtest-p0-sol.md(P0-1 futu 历史不可拉)
- 裁决书:~/.claude/plans/mutable-nibbling-music.md(数据质量卡 schema)

## State

- [x] 腾讯日 K 回填工具(00700 幂等回填)
- [x] 港股期权历史接口调查
- [x] 数据质量卡 source/adjusted 标注
- [x] 真实 PG 回填验收(00700 ≥300 交易日)
- [x] #66 评审 P1/P2 修复（形成 K、CLI smoke、`/v1/bars` 契约）

## Evidence（2026-08-14）

- 新增 `wbot ingest tencent`：固定腾讯 `qfq` → canonical `adjust=fwd,source=tencent`，支持 HK/US/SH/SZ 市场代码映射、进程内请求间隔 ≥1s、429/5xx/网络失败指数退避、范围过滤与响应内同日去重；落库复用 `RunIngestion` 的事务和 `ON CONFLICT DO NOTHING`。
- 腾讯真实接口可能额外返回北京时间今日的盘中形成 K；默认在 source 边界剔除该末行，次日运行再补完整值，`-include-forming` 可显式恢复旧行为。2026-08-14 验收 dry-run 返回 1000 个已完成 bars，止于北京时间 2026-08-13；`US.JD` 如实返回最近 1 个已完成交易日并输出「腾讯美股仅当日,历史靠每日积累」。
- 真实 PG 中 `source=tencent,adjust=fwd` 的 `HK.00700` 为 `1001 rows / 1001 distinct ts`；连续两次执行 `scripts/accept-tencent-datafill.sh` 均为 `ALL 8 CHECKS PASSED`，第二次行数不增长。
- 回测同日多源固定选择 `futu → tencent → 其他 source`，腾讯只补 Futu 缺日。真实 PG 报告共消费 5462 日，其中 `futu/fwd=5458`、`tencent/qfq=4`；JSON/HTML `data_quality` 均显示标的 bars provenance，并单独列出实际消费的期权 snapshot source。
- 港股期权调查结论：腾讯免费端点不能发现/识别已上市 TCH 合约，历史 K 为空，未实现伪造回填；期权面继续由实时 snapshot 向未来积累。详见 `doc/issues/2026-08-14-tencent-hk-option-api-investigation.md`。
- 验证通过：目标包单测、真实 PG ingest/backtestexec 集成测、验收脚本连续两轮，以及完整 `scripts/verify.sh`（含 gofmt/test/vet/race/staticcheck/CLI smoke）。
- #66 评审修复验证：单测覆盖默认剔除北京时间今日末行与 `-include-forming` 保留两分支；CI/本地 smoke 均覆盖 `ingest tencent -h` 及非法 `-count 0` 精确 exit 2；`/v1/bars` HTTP 响应和文档同步暴露逐 bar `source`/`adjusted`，并声明 `futu → tencent → 其他字典序` 去重语义。

## Next

提交 #66 评审修复供 reviewer 复审；生产调度由 cron/systemd 每日一次运行，供美股数据从当前日起逐日积累。
