# Wheel 实时运行切片 C:wheel 实时运行器 + serve 集成

- **id**: `2026-08-11-wheel-live-slice-c`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

新包 `internal/wheelrun/runner.go`(依赖切片 B 的 `positions.go` 与 `futu.OptionQuotes`):`RunOnce(ctx)` 对每个 wheel 绑定执行完整链路——List watchlist 绑定 → `wheelstore.LatestConfig` → 现价(`/api/quote`,复用 SnapshotLimit)→ sim 持仓 → `OptionChain`(min/max_dte 窗口)→ `OptionQuotes` → 组装 `wheel.DecisionInput` → `wheel.Evaluate` → 映射 `SignalRecord` → `AppendSignal` → watchlist status 同步。`Run(ctx, interval)`:首轮立即跑,单协程顺序,错误 log 不中断。serve 集成:`--wheel-run`(默认 false)、`--wheel-interval`(默认 5m)、`--wheel-env`(默认 sim);main.go:326 后 `go startWheelRunner(...)`(新 cmd/wbot/wheel_scheduler.go 组装依赖)。

## Constraints

- **不碰**其他切片文件:`internal/futu/`(切片 B)、`internal/llmreview/`(切片 D)、telegram(切片 E)。信号只落库不推送(推送与 LLM 审核是 C/D 之后的事,由 E 接入)。
- watchlist status 同步:新增 `SetExecutionStatus(ctx, db, symbol, status, reason)`(internal/watchlist/watchlist.go):合法值 ∈ {READY, DATA_BLOCKED, NEEDS_RECONFIGURATION}(参照 CHECK 允许集),READY 清空 reason,DATA_BLOCKED 填 blocked 摘要(reason 首条);非法 status 返回错误。UPDATE watchlist SET execution_status=$2, invalidation_reason=$3, updated_at=now() WHERE symbol=$1。
- 信号能力状态映射:Evaluate 出的 ALERT(能力 READY)→ status READY;HOLD + blocked 原因 → DATA_BLOCKED + reason 摘要(取首个 blocker);SignalRecord.BlockedBy 取 wheel.Signal 的 blocked 字段(需看 Signal 结构有哪些字段;BlockedBy 空且能力非 READY 时至少落一条 "no complete quote snapshot" 兜底,满足 validateSignal 的 DATA_BLOCKED 必须非空 blocker 契约)。
- 无现价(网关不可用)→ 不跑该 symbol,log 记录继续下一个;整轮失败不 panic。
- 单协程顺序执行,无并发竞态;限流全部走 SnapshotLimit。
- 遵守 self-documenting-code(注释 ≤1 行)、vibe-coding 八荣八耻。

## Links

- Driven-By: 用户指令 2026-08-11「wheel 策略先实际应用到 futu 模拟盘运行起来,按默认参数即可」(serve 定时常驻)
- Plan: `/home/jiayu/.claude/plans/mutable-nibbling-music.md` 切片 C
- Branch: `feat/slice-c-wheel-runner`(worktree `.claude/worktrees/slice-c-wheel-runner`)
- Depends-On: 切片 B(`futu.OptionQuotes` + `wheelrun.positions.go` 接口,以 B 交付为准)

## State

- **status**: `queued`
- **last step**: 主会话已探索:main.go:324-326(datacheck 协程后加 runner)、flags 区 232-241(datacheck 系列为模式参考)、watchlist.Upsert 写死 DATA_BLOCKED(无 status 更新方法,需新增 SetExecutionStatus)、AppendSignal 契约(store.go:558,ALERT 需 READY+6 字段+候选,DATA_BLOCKED 需非空 blocked)、wheel.Evaluate(cfg, DecisionInput) 输入字段全清单(CurrentPrice/AsOf/StockShares/FuturesEquivalentShares/Positions/DailyOrders/ExtremeDay/CashAvailable/HasCashAvailable)、futu Quote 模式(client.go:92)、datacheck_scheduler.go:67(startDataCheckScheduler 为 serve 协程模式参考)

## Next

等切片 B 交付后:在 worktree `.claude/worktrees/slice-c-wheel-runner`(branch `feat/slice-c-wheel-runner`,基于 B 合入后 HEAD)实现 runner + SetExecutionStatus + serve 集成 + 单测(fake quoter/positions,参照 wheel 现有测试模式;SetExecutionStatus 单测)→ `scripts/verify.sh` 等价自测 → 独立分支提交(push)→ 报告改动文件/测试结果/遗留问题。
