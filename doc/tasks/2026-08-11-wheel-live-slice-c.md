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

- **status**: `delivered`(评审批准,已合入基线 `b3a787c` merge)
- **last step**: 主会话已探索:main.go:324-326(datacheck 协程后加 runner)、flags 区 232-241(datacheck 系列为模式参考)、watchlist.Upsert 写死 DATA_BLOCKED(无 status 更新方法,需新增 SetExecutionStatus)、AppendSignal 契约(store.go:558,ALERT 需 READY+6 字段+候选,DATA_BLOCKED 需非空 blocked)、wheel.Evaluate(cfg, DecisionInput) 输入字段全清单(CurrentPrice/AsOf/StockShares/FuturesEquivalentShares/Positions/DailyOrders/ExtremeDay/CashAvailable/HasCashAvailable)、futu Quote 模式(client.go:92)、datacheck_scheduler.go:67(startDataCheckScheduler 为 serve 协程模式参考)

## 评审结论(2026-08-11,reviewer 批准)

- **结论**:批准合入;达到可使用阶段;功能类型 **feature**;无 P0/P1(代码已于 b3a787c 合入基线,评审为合入后收尾)
- 已验证:`go build/vet/test ./...`、`-race` 相关包、5 个 runner 场景测试真实断言
- **P2 移交**:
  1. CI wheel live 冒烟缺失:`serve -wheel-run`(死网关)+ `/v1/health` 200 断言不在任何 acceptance 脚本 → 并入切片 F(accept-wheel-live.sh 已有「网关不可用跑 DATA_BLOCKED 路径」约束,补 /v1/health 断言)
  2. `cmd/wbot` 适配器零单测:`qualifySymbol` 表驱动(HK/US/SH/SZ)、`parseWheelEnv`、`futuQuoter.Quote` s2c fixture 解析、`dailyOrders` 边界(昨日/今日 ALERT 计数)→ 并入切片 F
  3. `-wheel-interval 0/负值` 仅 goroutine 内拒绝(循环静默失效)→ 并入切片 F,flag 解析处 fail-fast(exit 2)
- **P3 观察**:HasCashAvailable 恒 false(PUT 方向必 DATA_BLOCKED,fail-closed 刻意合理);持仓每 symbol 重拉/每 pass 新建 proto 连接(可上提一次);price<=0 直接返回 vs NaN 穿透落 DATA_BLOCKED 行为不一致;每 pass 每 symbol 落 HOLD(默认 5m 约 288 条/日/标的,可考虑仅状态变化落库);proto 无 per-call 超时;日志无等级(并入分级日志路线)

## Next

(收口)切片 F 需吸收上述 P2 移交项;P3 观察项排期评估(日志分级/落库节流可进 backlog)。
