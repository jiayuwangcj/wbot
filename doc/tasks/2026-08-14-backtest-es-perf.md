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

---

# 追加:评审 + 合入 + 新口径重训(2026-08-14)

## Reviewer 评审结论(有条件合入,已全部处理)

P0 无;行权 buy-in 记账数学验证通过(covered 按原成本、缺口按 buy-in 价,每股恰好入账一次)。功能类型:**feature(含 1 处 bugfix:行权裸卖空修正)**。

| 级别 | 发现 | 处置 |
| --- | --- | --- |
| P1 | accept-backtest-report.sh 硬编码 schema 1.2 + 旧 total_return 字段 → CI 必红(实测 3/14 FAIL) | 已修:同步 schema 1.3 + realized_return/mark_return 字段 |
| P1 | accept-backtest.sh 断言 CLI total_return= → 必红 | 已修:同步 realized_return=/mark_return= 形状 |
| P1 | 非 wheel 基准(hold/buy-hold)capability 恒 DATA_BLOCKED → net_return 全 null,失去收益头条 | 已修:非 wheel 保留市值收益口径(无战略参数,不受已实现切换影响);wheel 仍走已实现 |
| P1 工具 | verify.sh accept 段落忽略退出码 → 假绿掩盖 CI 必红 | 已修:run_accept 逐脚本退出码把关 |
| P2 | live wheelrun 未接 LastEffectiveFillPrice → 回测激活 stock_switch/move_interval 门而实盘休眠 | 排期:另立任务接线 live 订单成交价 |
| P2 | hkexHistoricalOptionCycleComplete 闸门仍按 DTE 10 证明覆盖,ES 已允许 45 | 排期:闸门按 wheel.MaxWheelDTE 对齐 |
| P2 | StockSuggestion BUY 无现金钳制(SELL 有) | 排期:BUY 钳制到可负担股数 |
| P2 | DTE 窗 5..10→5..45 属独立产品变更,建议拆分提交 | 随批合入,任务记录标注 |

## 合入

- `b83a04c`(fix/backtest-es-perf)→ main **fast-forward** 合入(3fd2fba..b83a04c,共 3 提交:6c851e5/db49c79/b83a04c)
- 主仓库工作树在途 doc 修改 stash 暂存 → 合入后 pop 恢复,无冲突

## 新口径重训(2026-08-14,reward-2.0 已实现)

命令:HK.00700 全窗(2022-03-09..2026-08-13)1d/fwd、cash 1,000,000、fee 3、seed 123、-params 战略参数 {full_position_price:400, zero_position_price:600, max_inventory:1200}、-train {move_interval_pct:[0.005,0.03], min_option_quality:[0.5,0.8]}、population 20、max_generations 40、budget 840。PG:wbot-pg-ci-test(容器 IP 192.168.215.2:5432)。

结果(报告 bt-HK.00700-123-7e5d56c3,schema 1.3):
- **早停 9 代 / 204 evals / 840 预算未用满**;train_seed=7045419309623635055, test_seed=1912771454634982101
- **样本外(测试窗,已实现口径):net_return +14.01%,年化 +3.00%,最大回撤 5.93%,未成交率 9.68%,市值标记 +7.24%**,final_equity 1,072,381(初始 100 万)
- **buy-hold 基线 +27.44% → excess −13.43%**
- **无可推荐参数:样本外多 seed 未稳定胜出 buy-hold 基线(candidates 为空)**
- 费用拖累仅 168 HKD(0.017%):交易极稀疏,期权 21/张成本占比可忽略

解读:新口径(只看已实现)下 ES 诚实输出「无可推荐参数」——2022-03 至今腾讯上行期,满仓持有 +27.4%,wheel 已实现 +14.0%(防守型,回撤 5.9%)。战术参数无法在样本外稳定跑赢 buy-hold,这正是评价口径剥离浮盈后的真实结论(此前 reward-1.0 市值口径会给出参数建议,但建议与战略参数耦合)。不推荐发布参数变更;回测工具链照常可用,报告可复现(同 ID 幂等)。
