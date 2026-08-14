# 收拢报告:supervisor-fc 外派会话(2026-08-14)

外派任务全部完成,本会话已退出。主会话可据此继续。

## #65 回测 P1:S7 Discord 推送 + ES 训练门 — completed

- 合入 `e4f6a69`(fix/backtest-p1 → feat/llm-signal-endpoint,--no-ff),收口 `beaddd5`,已 push
- 评审:feature,无 P0/P1;P2 1 项(推送失败退出码分层 permanent vs transient)记排期;P3 观察
- #64(ES -train 零覆盖快速失败)随片完成
- 验收:verify 全绿、accept-backtest-push 13/13、真实 PG 集成测通过;**未向真实 Discord 发送**(留 operator 部署后 smoke)

## #66 腾讯日 K 数据回填 — completed

- **派单冲突处置**:本会话派单与主会话侧派单并发,已杀掉本会话重复单(codex 单飞纪律),对方 codex(2554053,`-m gpt-5.6-sol`)单飞完成 `73607a7`
- 评审两轮:
  1. 首轮**有条件合入**(feature,含 QueryBars 多源去重 bugfix):P1 形成 K 冻结(腾讯端点总返回当日盘中 partial,ON CONFLICT DO NOTHING → 日终值永不落库)
  2. codex 修复 `e394a84`(默认剔除形成 K + `-include-forming` + CLI smoke + /v1/bars 契约)→ 复评**合入(无条件)**
- telegram 同族竞态随修 `f19a8b0`(Claude 署名)
- 合入 `9d0813c`,收口 `f4cbf01`,已 push;verify 全绿、accept-tencent-datafill 8/8 两轮、真实 PG 1001 行幂等
- **关键结论**:腾讯免费接口 00700 日 K 可用(2022-07-22 至今 1001 条,1000+ 交易日);US.JD 仅当日(历史靠积累);**港股期权历史接口不可用**(qt.gtimg.cn 不识别 TCH 合约,期权面继续靠 snapshot 积累,见 doc/issues/2026-08-14-tencent-hk-option-api-investigation.md)
- worktree backtest-datafill 已清理;分支 fix/backtest-datafill 保留远端

## 遗留排期项(不阻塞合入)

- #65 P2:推送失败退出码分层(permanent vs transient,需产品确认)
- #66 P2:cron 调度器落地时固化「收盘后运行」约束(美股积累 04:00–21:30 北京时间,避开 00:00–04:00 US 盘中盲区);真实 PG 中 2026-08-14 tencent partial 行(close=444.2/vol=8.39M)可运维随手清理
- #66 P3:accept-tencent-datafill dry-run 段 CI 化评估;腾讯 volume float64→int64 >2^53 精度观察项

## 遗留问题:US.JD 期权链拉取失败(#73 DTE 放开副作用,待主会话处理)

- **现象**(serve 日志):`wheelrun: US.JD: option chain: GetOptionChain: requested period (2764800 seconds) exceeds 30 days + 1 hour (2595600s) limit`,美股时段每轮出现
- **根因**:`internal/wheelrun/runner.go:203` 直接 `OptionChain(ctx, symbol, now.AddDate(0,0,cfg.MinDTE), now.AddDate(0,0,cfg.MaxDTE))`;#73 将 max_dte 上限 10→45 后,US.JD 窗口 = 32 天(2764800s)> futu 网关 30 天 + 1 小时限制(2595600s),无 clamp/分段
- **影响**:runSymbol 返回错误 → 该标的本轮**静默跳过**(无 ALERT/HOLD,也非 DATA_BLOCKED),即美股时段 US.JD 提醒功能失效;HK.00700 不受影响(实测 6h 内 0 错误)
- **远端状态**:主基线已推进至 5cff3c3(#70 合并),wheelrun 无相关修复——**主会话尚未处理**
- **修复建议**(留给主会话派单):① runner 侧窗口 clamp/分段拉取(如按 futu 上限截断,覆盖 min_dte 之外的合约做补偿逻辑),或 ② #73 四层约束在运行时侧补齐校验;与 #84(训练参数实装 00700 + JD 尝试运行)强关联——JD 实盘运行必撞此问题

## 状态备注

- serve 信号流正常(806–815 ALERT/HOLD 持续,LLM gate 瞬时失败重试机制按预期工作,300s 超时生效)
- 本地未跟踪文件备份:/tmp/backtest-p1-s7-local-dispatch-record.md、/tmp/backtest-datafill-local-dispatch-record.md(派单时本地创建的旧版任务记录,仓库内已由 sol/评审更新版本替代)
