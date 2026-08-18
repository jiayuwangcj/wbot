# 回测内存优化：候选详情不常驻 + 滚动切块执行（2026-08-15 老板指令）

## Goal

全窗口 00700 1m 回测不再 OOM（2026-08-15 实测峰值 ~9.7GB anon-rss 杀掉 tmux）。修两个根源：

1. **候选详情不常驻**：每根 bar 的 `restoreExpiryRejectedCandidates` 把整条期权链（~300 合约 × ~400B）物化进 `Signals[].CandidateDetails` 常驻内存 → 全窗口 ~100 万 bar 常驻 ~9GB
2. **滚动切块执行**：输入数据从「全窗口一次载入」→「逐块载入 + 状态跨块延续」，极长窗口也能跑

**老板决策（2026-08-15）**：候选详情默认不物化；报告/审计需要时 `-trace-candidates` 重跑再取（回测确定性保证可重生成）；不需要的不要常驻。

## 改动面

### Stage 1 — 候选详情不常驻（主修）

| 文件 | 改动 |
| --- | --- |
| `internal/strategy/strategy.go` | `WheelStrategy` 加导出字段 `TraceCandidates bool`（默认 false）；485 行门控 `if s.TraceCandidates && !signal.ClosePosition { restoreExpiryRejectedCandidates(...) }` |
| `internal/backtestexec/backtestexec.go` | `Options.TraceCandidates bool`；`Build` 构造 `*WheelStrategy` 后设置 |
| `cmd/wbot/main.go` | `-trace-candidates` flag（默认 false）→ 单跑 Options |
| `cmd/wbot/backtest_train.go` / `backtest_tune.go` | 显式 `TraceCandidates: true`（buildTrajectory 用候选构建 RL 轨迹） |

**关键事实**：
- `-report` 摘要/指标/曲线/DataQuality/capability 只读 `CapabilityStatus`/`BlockedBy`（`backtestreport/report.go:384`）——不需要候选详情
- 实时 wheelrun 走 `wheel.Evaluate` 直连（`wheelrun/runner.go:473`），不经 `WheelStrategy.OnBar`——trace 开关只影响回测路径
- `restoreExpiryRejectedCandidates` 全仓库只有 strategy.go:485 一处调用
- 测试面：`CandidateDetails` 断言仅 `internal/strategy/return_boost_test.go` 等 1-2 处，需补 `TraceCandidates: true`

### Stage 2 — 滚动切块执行（压输入数据）

| 文件 | 改动 |
| --- | --- |
| `internal/backtest/backtest.go` | 抽出每 bar 内环为可复用 session（持 `st`/strategy/累加器），新增 `Process(bars, opts)`；单跑与切块共用（**单跑路径行为逐位不变**） |
| `internal/backtestexec/backtestexec.go` | 新增 chunked 路径：按月切窗口；逐块 `QueryBars` + `QueryUnderlyingQuoteSnapshots([chunkStart−lookback, chunkEnd])`；共享 State + 策略实例跨块延续；lookback = `quoteRangeStart`（min_iv_rank>0 时 1yr 覆盖 MaxDTE≤45 持仓合约价格史）；sourceHash 流式 |
| `cmd/wbot/main.go` | `-chunk 30d` flag（默认关=单跑，行为不变） |

## Constraints

- worktree: `.claude/worktrees/backtest-mem-rolling`（分支 `fix/backtest-mem-rolling`，基于 main=be051f2）
- **确定性铁律**：同 seed 同 params 同数据 → 输出逐位一致；切块 vs 单遍 diff（含 sourceHash）逐位一致为验收门槛，不一致加宽 lookback 或修边界
- **不污染实时路径**：wheelrun 实时评估不受影响
- 提交前 `scripts/verify.sh` 全绿
- 编码：codex 欠费(usage limit,2026-08-15) → Claude 侧执行；提交署名 `Co-Authored-By: Claude <noreply@anthropic.com>`
- **跑数据纪律（老板 2026-08-15 指令）**：牵涉大量数据先小数据观察内存稳定再全量；全量前先 `/usr/bin/time -v` 小窗口估内存

## Links

- OOM 定位：wbot 会话 2026-08-15（堆画像 `go tool pprof`：restoreExpiryRejectedCandidates 2209MB / makeSignalTrace 1614MB / wheel.Evaluate 1580MB 累计）
- 数据链路缺口：`doc/tasks/2026-08-15-intraday-bars-fill.md`
- 回测契约：`doc/BACKTEST.md`；实时路径 `internal/wheelrun/runner.go`

## State

- [x] 建 worktree + 任务记录
- [x] Stage 1：候选详情不常驻（**实证强化**：gate restore + `makeSignalTrace` 门控 CandidateDetails 拷贝；见下）
- [x] Stage 2：滚动切块执行（**实现修正**：期权快照集一次载入保 IVRank 确定性，见下）
- [x] 验证（verify 全绿 + 单遍 vs 切块逐位 diff + 小数据先验内存 + 全窗 RSS）
- [x] 文档（doc/BACKTEST.md「内存有界执行」节）
- [ ] reviewer 评审（bugfix）+ 合入 main

## 实现修正（2026-08-15 coder）：IVRank 确定性 → 快照集一次载入

原设计逐块 `QueryUnderlyingQuoteSnapshots([块起点−lookback, 块终点])`。实测发现该设计破坏确定性铁律：

- `attachIVRanks` 用**同一 opts 切片内**的批次算每批次的 1yr IV percentile。逐块截断窗口使已拥有批次的 IVRank 与单遍（全窗 `[quoteRangeStart, to]`）不一致；IVRank 字段序列化进批次 JSON → 是 `sourceHash` 的一部分 → 单遍 vs 切块 hash 不同。
- 合成单测未暴露（数据 <20 观测 → 全 0 rank）；真实 DB 2 周窗口暴露（`input_snapshot_hash` 不一致）。

**修正**：`RunChunked` 的期权快照集**一次载入**（与 `Prepare` 相同的 `[quoteRangeStart(o.From), o.To]` 窗口），构建 `fullOpts` 后每块 `Process(bars, fullOpts, ownedFrom)` 复用。快照集远小于 bars（HK.00700 全量 ~123k rows / ~20MB vs 92 万 bars / ~92MB），bars 仍逐块流式——内存有界目标不变，且 IVRank/批次/`sourceHash` 按构造与单遍逐位一致。`chunkLookback`/`chunkBufferFloor` 移除（不再需要逐块快照回看）。

**实测（HK.00700 1m, cash 100 万, `-adjust none`）**：
- 2 周（6620 bars）单遍 vs `-chunk 3d/7d/30d`：`-report` JSON md5 逐位一致，`report_id`/`input_snapshot_hash` 相同
- 2 周含成交窗口（`full=450 zero=550`，30 次成交尝试/3 未成交）：单遍 vs `-chunk 7d` 逐位一致
- 2 周 `-save` + `-export json` 全量结果（6620 equity pts + metrics + source_hash）：剔除 DB id/`created_at` 后逐位一致
- 2 周 chunked peak RSS 44.6MB；**全窗（918380 bars, 2015→2026, `-chunk 30d`）peak RSS 1.215GB, killed=0 跑完**（单遍 Stage 1 实测 1.67GB；原 OOM ~9.7GB）

## 实证结论（2026-08-15 主会话先验，决定 Stage 1 强化）

**计划原假设错误**：`restoreExpiryRejectedCandidates` 门控后 `signal.Candidates` 并非「已接受候选（少而小）」——`wheel.Evaluate` 把 **DTE 窗内全部候选（含 rejected + reasons 字符串）** 写入 `signal.Candidates`，`makeSignalTrace` 再整体复制。实测 GC trace：3310 bars 常驻堆 ~112MB（~34KB/bar），925k 全窗 → ~31GB，**仅 gate restore 仍 OOM**。

**强化**：`makeSignalTrace` 经 `candidateDetailTracer`（`WheelStrategy.KeepCandidateDetails() bool`）门控 `CandidateDetails` 拷贝——trace off 时信号只留决策元数据（action/reason/capability/inventory/选中合约），不再复制整条候选链。

**实测（HK.00700 1m，cash 100万）**：
- 2 周（3310 bars）：trace off **48MB** vs trace on 190MB；结果逐位一致（final_equity=981268）
- **全窗（918049 bars, 2015→2026, `-adjust none`）：peak RSS 1.67GB，killed=0 跑完**（原 9.7GB OOM）
- `-adjust fwd` 历史复权因子表仅覆盖近期 → 全历史用 `-adjust none`（数据链路缺口，非本次改动面）

Stage 2（滚动切块）价值重估：Stage 1 已达成内存目标（<2GB），Stage 2 提供更长窗口/多符号的峰值压降与「滚动」机制（用户明确要的形态）。

## Reviewer 评审（2026-08-15）

- **结论**：有条件合入。**功能类型判定：feature**（含 bugfix 成分）——新增 `-trace-candidates`/`-chunk` flag、`WheelStrategy.TraceCandidates`/`KeepCandidateDetails`/`backtest.Session`/`RunChunked` 导出 API，且默认 `signals[].candidate_details` 由常驻改默认空。与任务记录此前「bugfix」预期不符，发布按 feature 合批。
- **P1（阻断 → 已修复验证）**：`-chunk` 块边界 bar 被重复处理——`QueryBars` 两端闭区间 + `RunChunked` 块推进 `cur=next` 后下一块 `ts >= next`，块边界恰为 bar 时间戳时该 bar 重复入 Process/hash。主会话独立复现：`-from 2015-04-16T01:30:00Z`（=首根 bar）+ `-chunk 3d`，单遍 bars=3641 vs 切块 3643。reviewer DB 核验全窗 138 边界中 91 个命中 bar。**修复 `24f6e57`**：`ingest.QueryBarsExclusiveEnd`（`ts < to` 独占上界），`RunChunked` 非末块 `[cur,next)` / 末块 `[cur,to]` 闭区间；单遍路径 `QueryBars` 调用不变。回归：`chunkEnd` 分块算术单测（边界 bar 恰被一块覆盖）+ DB 集成 `TestRunChunkedBoundaryBarNotDuplicated`。**主会话独立复核**：边界窗口单遍 vs `-chunk 3d` bars=3641=3641、`-report` JSON md5 逐位一致。
- **P2**：`Session.Process` 当前已 `ValidateBars`（75-77）在 `applyOptions`（78）之前，无需改动。
- **P3（排期，不入本次收口）**：`-chunk 1ns` 最小块时长校验、`RunChunked` 逐块进度日志、`-chunk`+`-export` 组合提示、`-save` 持久化 trace 开关、DB 集成 `accept-backtest-chunk.sh`、`-chunk`+`-trace-candidates` 内存警告。另有既有 flake `TestCallbackYesDoubleConfirmRejected`（非本改动引入）。
- 通过项：API/CLI 契约（flag 组合校验、help 文本）、实时路径零污染、密钥安全、CI skip 合法性、提交粒度（提交分层清晰）。

## Next

- [ ] 主会话 verify 全绿后合入 main：`da4051a`（Stage 1）+ `d5376a0`（Stage 2）+ `82ea710`（docs）+ `24f6e57`（P1 修复）
- [ ] 任务记录收口（feature 判定已记，发布按合批）
