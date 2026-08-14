# 回测 ES 性能优化 B3:并行评估 + 一次多组参数(2026-08-14 老板指令:优化全部落地)

## Goal

#69(B1+B2)合入后实施 benchmark 报告 B3(评审 P1):ES 种群与 held-out 评估并行化,并支持一次运行多组参数共享数据加载。目标:ES 训练 53.5s(最坏 4-5min)→ B1+B2 后 ~1min → B3 后 ~10-15s 级(8 核);用户提问「能否并行一次跑多组参数提高数据利用率」的直接落地。

## 背景:数据利用率的两个层面(用户 2026-08-14 提问)

1. **一次加载多次复用**(#69 B1,已完成):每窗口 Prepare 一次,全部评估共享——数据利用率核心
2. **一次跑多组参数并行**(本任务):多个 (params, window, seed) 评估是纯函数、零共享状态 → worker pool 并行,墙钟 ≈ 串行 ÷ 核数

## B3 规格

- 位置:cmd/wbot/backtest_train.go evaluator(已由 #69 改为 Prepare/RunPrepared 结构);backtestes Search 调用点
- 改法:
  - worker pool(如 8 workers)并行种群评估与 held-out 评估;结果**按序收集**(确定性铁律:同 seed 同 params 同数据 → 输出逐位一致)
  - OptionsData 共享:RunSeed 复制防共享突变(B1 半成品已注释,确认并行下无数据竞争:bars/options 只读)
  - 固定参数 CLI 支持一次多组参数(如 `-params A,B,C` 或参数文件),共享一次 Prepare 数据 + 并行评估——参数敏感性分析一次跑完
- 验证:
  - 同 seed 同 params 同数据 → 输出与串行逐位一致(ES 报告、成交记录、metrics 不可漂移)
  - `go test -race` 覆盖并行路径(race 检测)
  - 固定参数多组:同一窗口并行跑 4 组与串行跑 4 组结果一致
  - 实时 serve 路径无回归(实时评估仍是单信号,不受并行影响)
  - benchmark:同命令同窗口 wall time 前后对比记录到任务记录
- 提交前 scripts/verify.sh 全绿;署名按实际编写模型

## Constraints

- worktree:`.claude/worktrees/backtest-es-perf`(复用,基于 #69 合入后的主基线开分支)
- 确定性铁律 + 实时路径无回归(同 #69)
- 报告 schema、CLI 契约不变;`-params` 多组语法保持向后兼容(单组行为不变)
- codex 单飞:#69 合入后再派

## Links

- 提效报告:efficiency benchmark 2026-08-14(B3 为 P1,建议「goroutine worker pool(如 8 workers)并行种群与 held-out 评估,结果按序收集」)
- 任务记录:#69 doc/tasks/2026-08-14-backtest-es-perf.md(前置,已合入)

## State

- [x] #69 已合入，基线 `6e90660`
- [x] worker pool 并行种群/held-out 评估
- [x] 固定参数 CLI 一次多组参数
- [x] 确定性验证(并行 vs 串行逐位一致)+ race + benchmark 对比
- [x] verify 全绿

## Evidence

- Worker pool: `internal/backtestes` 固定 `EvaluationWorkers=8`；ES 每代种群与验证评估、训练样本外 `(candidate, seed)` 任务和固定参数批量评估均通过有序 `ParallelMap`，结果按输入槽位回收，完成顺序不会影响报告。
- 竞态防护:训练入口先顺序 Prepare train/validation/sample-out 三个窗口，再只读共享 `prepared` map；`Prepared.RunPrepared` 对每次评估复制 `OptionsData` wrapper 并覆盖独立 `RunSeed`，bars/options 数据只读共享。
- 确定性: `TestSearchParallelMatchesSerial` 对并行/单 worker ES JSON 逐位比较；`TestRunPreparedConcurrentEvaluationsMatchSerial` 对共享 OptionsData 的并行结果与串行结果逐位比较；并发 pool 测试断言输入顺序和最多 8 workers。
- CLI: `-params` 单组行为保持不变；可重复传多个 JSON，或传 `-params '[{...},{...}]'`。DB 固定参数批量实跑 4 组，输出 `fixed_params=4 workers=8`，四组结果按参数输入顺序返回；逐组串行重跑的 `final_equity/total_return/max_drawdown/fees/unfilled` 与批量结果逐项一致；多标的/ES 组合按契约拒绝。
- 实时路径: `go test -race ./...` 包含 `internal/wheelrun` 全套实时评估测试通过；本任务只改回测 CLI/ES 与文档，serve 路径未接入 worker pool。
- Benchmark command (same PostgreSQL test container, same HK.00700 `1d/fwd` window `2025-02-10..2026-08-13`, cash `1,000,000`, fee `3`, seed `123`, train ranges `move_interval_pct=[0.005,0.03]`, `min_option_quality=[0.5,0.8]`, population `20`, max generations `40`, budget `840`): #69 binary `6e90660` wall `11.01s`; B3 binary wall `3.43s`; both stopped after 9 generations with `evaluations=204` and the same `train_seed`, `test_seed`, `sample_out_return=-0.001345`. This is `3.21x` faster / `68.8%` lower wall time on the paired run; the prior #69 report also recorded a sequential `9.47s` warm run on the same workload, so host/DB cache variation is expected.
- Verification: `scripts/verify.sh` passed, including frontend build, full Go test/vet/race/staticcheck, cross-build, CLI smoke, and acceptance scripts.

## Next

主会话评审 `fix/backtest-es-perf` 后合入；按发布流程合批，再用真实 00700 数据继续参数实装/调优。
