# wheelrun 异步审核 + 重复候选抑制(2026-08-14 老板指令「现在就做」)

## Goal

老板 2026-08-14 指令:立即修复「LLM 审核阻塞轮询循环 + 同候选每轮重复 ALERT」。背景:868/870/871 实锤 reviewAlert 同步调用(wheelrun.Runner 单 goroutine 顺序循环,runSymbol 在 ALERT 时同步审核,deepseek 单次 3-4 分钟 → 阻塞期间所有 symbol 停摆);JD 28/29P 候选持续满足条件 → 每轮 pass 重复 ALERT → 每轮重复 4 分钟审核。

落地:
- **P0-A 异步审核**:reviewAlert 移出 run 循环——有界队列 + 2 worker + per-symbol 在途去重(同 symbol 审核在途时新 ALERT 跳过审核,下轮窗口外再审)。runner 立即继续;审核结果照常落 LLM_REVIEW/REJECTED,推送器机制不变。
- **P0-B 重复候选抑制**:同 symbol 同合约(sig.Quote.Symbol)30 分钟窗口内重复 ALERT 降为 HOLD(reason 标注「重复候选抑制」);被抑制轮不滚动窗口,窗口期满重新 ALERT 再审。
- **配套**:推送器 signalFreshWindow 10m → 15m(异步审核排队 + 300s 超时 + 重试一次最坏 ~10 分钟 > 旧窗口,防丢信号;telegram/discord 共用常量)。

## Constraints

- worktree: `.claude/worktrees/wheel-review-async`(main,分支 worktree-wheel-review-async;与 PR #338 backtest-es-perf 隔离)
- 资金安全链路:审核是推送前置闸门,任何路径不得绕过;LLM 不可用时 ALERT 信号照常落库但不推(现状语义,不变)
- 队列满/worker 异常:防御性丢弃 + 日志(下轮窗口外 ALERT 会再审),绝不阻塞 run 循环
- serve 关闭:ctx 取消 → 在途审核失败落 REJECTED(与现状同步语义一致);Close 等 worker 退出(不泄漏)
- 提交前 scripts/verify.sh 全绿;reviewer 评审后合入;署名 Claude

## Links

- 根因:doc/tasks/2026-08-14-backtest-premium-return.md(CI 修复记录旁);生产信号 868/870/871/872
- 推送器:cmd/wbot/telegram_scheduler.go(signalFreshWindow/llmReviewRetryWindow 常量区;discord_scheduler.go 复用)

## 实施

1. `internal/wheelrun/runner.go`:
   - Runner 加 reviewCh(chan reviewTask, cap 8)、reviewInflight(map[symbol]bool)、reviewMu、reviewWG、closeOnce、lastAlert(map[symbol]lastAlertInfo)
   - reviewWorkers = 2(单飞串行会让多 symbol 同开时排队超 fresh 窗口);repeatAlertWindow = 30m
   - NewRunner 起 2 个 reviewWorker;Run 末尾 defer Close
   - runSymbol ALERT 分支:先 suppressRepeatAlert(命中 → record.Action=HOLD + Reason 标注),再 enqueueReview(LLMReviewer nil 或无审核 → 跳过)
   - reviewWorker:for range reviewCh → reviewAlert(保持原同步函数签名)→ 清 in-flight
   - enqueueReview:per-symbol 在途去重 + select 非阻塞入队(满 → 日志 + 重置在途)
2. `cmd/wbot/telegram_scheduler.go`:signalFreshWindow 10m → 15m(注释注明异步审核)
3. `internal/wheelrun/runner_test.go`:
   - fakeStore/fakeReviewer 加 mutex(worker 并发写,测试同步读,-race 必红)
   - TestRunOnceLLMGateStates / TestRunOnceLLMReviewVisibleToLatestLLMReview 改 waitFor 轮询(审核异步完成)
   - 新测试:① 审核不阻塞 RunOnce(blocking reviewer + 计时)② 同 symbol 在途去重(连续两次 RunOnce 只 1 次 Review)③ 重复候选抑制(同候选 2 次 → HOLD;换合约 → ALERT;窗口过期 → ALERT)

## State

- [ ] 实施 + 单测
- [ ] verify.sh 全绿
- [ ] reviewer 评审
- [ ] 合入 main + 部署(serve 重启;在途审核按 ctx 取消落 REJECTED)

## Next

评审 → 合入 → 部署。部署时机:交易时段重启 30s,可与 #338 合入后一起部署。
