# 回测 ES 性能优化 B1+B2(2026-08-14 老板指令:派 luna 实施 benchmark 发现的 P0 瓶颈)

## Goal

benchmark(efficiency 实测,HEAD cd9f16d)显示:单次回测 0.62s 很快;ES 训练是唯一重头(53.5s early-stop,最坏 4-5 分钟)。实施两个 P0 瓶颈修复,目标 ES 最坏 ~4-5min → ~1min 级。

## B1:ES 每次评估全量重载行情(load-once-per-window)

- 位置:cmd/wbot/backtest_train.go:87-96(evaluator 每 (params,window) 调 backtestexec.Run)→ internal/backtestexec/backtestexec.go:112-160 → internal/wheelstore/store.go:381-424(重查快照)→ internal/backtest/options_data.go:18-82(重建 OptionsData)
- 证据:数据与 params 无关(只依赖 symbol/from/to/limit),但每 eval 重拉 66.5k-121k 行;204 evals ≈ 13.6M 行 ≈ 2GB;窗口只有 3 个(train/valid/test),最多加载 3 次
- 改法:OptionsData 构建提出 evaluator,每窗口加载一次后注入各次评估;`OptionsData.RunSeed` 与数据解耦(seed 只影响 unfilled draw,不影响 chain/bars/batches)
- 验证:DB 查询 825 次 → 3 次;ES 结果与优化前逐位一致(同 seed 同输出)

## B2:逐 bar×逐合约无剪枝 + Validate 重活

- 位置:internal/strategy/strategy.go:331-420 OnBar → internal/wheel/wheel.go:655+ Evaluate 每 bar 遍历 ~264 合约;wheel.go:391-439 Validate 每 quote 构造 2 次 time.Date + DTE 失败路径 fmt.Errorf 分配
- 证据:1.3-1.5ms/bar ≈ 5µs/quote(远超 QualityScore ~50ns);DTE 窗 [5,10] 使多数合约走 DTE 拒绝路径(每次分配 error);min_option_quality 0→0.8 耗时几乎不变,证明开销在 Validate/DTE 路径
- 改法:① 加载时按 expiry 排序,每 bar 先按 [bar+min_dte, bar+max_dte] 预过滤(未来 5-10 天到期合约通常 1-2 个到期日 ≈ 20-60 个);② DTE 拒绝用预分配 sentinel 错误(拒绝原因字符串只在信号实际产出时构造);③ Validate 的 asOf/expiry 日期计算每 bar 提为 1 次、每合约每批预计算
- 验证:模拟阶段 3-8x;结果与优化前一致

## Constraints

- worktree: `.claude/worktrees/backtest-es-perf`(分支 fix/backtest-es-perf,基于主基线 cd9f16d)
- **确定性铁律:同 seed 同 params 同数据 → 输出逐位一致**(ES 报告、成交记录、metrics 不可漂移;新测试断言关键值)
- 报告 schema、CLI 契约不变;不碰实时链路(wheel 实时评估路径不受影响——B2 改动在 internal/strategy/wheel 通用代码,须确认实时 serve 路径无回归:实时评估是每 5 分钟单信号,剪枝不能漏掉实时路径的合约)
- 提交前 scripts/verify.sh 全绿;署名按实际编写模型(gpt-5.6-luna)
- benchmark 前后对比:同命令同窗口 wall time 记录(容器内或本机)

## Links

- 提效报告:efficiency benchmark 2026-08-14(B1/B2/B3/B4/B5/B6 全清单,实施顺序 B2→B1→B3→B4)
- 任务记录:doc/tasks/2026-08-14-backtest-p1-s7.md(ES 训练门已上线,确定性约定沿用)

## State

- [x] B2 模拟剪枝(expiry 预过滤 + sentinel + 日期提升)
- [x] B1 load-once-per-window
- [x] 确定性验证(同 seed 逐位一致)+ benchmark 前后对比
- [x] verify 全绿

## Evidence

- B1: `Prepare` loads one immutable bar/options snapshot per train/valid/test window; the evaluator and sample-out loop reuse `RunPrepared`. `RunSeed` is copied per run, so shared market data cannot carry seed state between evaluations.
- B2: quote snapshots build a canonical-order-preserving expiry index; backtest `OnBar` binary-searches `[bar+min_dte, bar+max_dte]`, while `wheel.Evaluate` computes validation dates once per bar and formats DTE rejection text only when materializing diagnostics. The adapter restores rejected-candidate diagnostics in canonical order, preserving the baseline ES report.
- Determinism: PostgreSQL test data, identical command/seed/params, and the same source hash produced identical `identity`, `result`, `terminal`, `data_quality`, generations, candidates, audit, risk, and all 76 trajectory steps. Only `train.duration_sec` is runtime wall-clock metadata.
- Realtime safety: `internal/wheelrun` continues to assemble the live quote set and call `wheel.Evaluate` directly; it does not use the backtest expiry index or `WheelStrategy.OnBar`. `go test ./internal/wheelrun` passed.
- Benchmark command: same HK.00700 1d/fwd window (`2025-02-10`–`2026-08-13`), ES population 16, 2 generations, budget 64, seed 123, PostgreSQL test container. Baseline `cd9f16d`: wall `23.595s`; final optimized binary: wall `6.150s` (warm-cache sequential run, 3.84x faster / 73.9% lower wall time; repeated runs varied with host/DB cache). Both returned `evaluations=49` and `sample_out_return=-0.05525`.
- Verification: `scripts/verify.sh` passed, including frontend build, gofmt, test, vet, race, staticcheck, cross-build, CLI smoke, and acceptance scripts.

## Next

主会话评审报告 → 合入 `fix/backtest-es-perf` → 按发布流程合批。
