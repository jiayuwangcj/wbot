# S3 未成交模拟与基础指标

**State**: 评审通过(有条件合入,2026-08-13)→ P1 修复完成(2026-08-13,Claude coder)→ 复核后合入
**分支/worktree**: feat/s3-unfilled-model @ .claude/worktrees/s3-unfilled-model(main 基线 3fd2fba)
**分支/worktree**: feat/s3-unfilled-model @ .claude/worktrees/s3-unfilled-model(main 基线 3fd2fba)

## Goal

期权「没成交是常态」:回测结算路径加**未成交模拟**(流动性启发式)与基础指标。裁决书: doc/BACKTEST_REPORT.md §3(口径)、doc/issues/draft-2026-08-13-backtest-rl-sol-eval.md §4(设计)。

## 契约

- **订单假设**(写进代码注释与报告):卖出 Put/Call、按 Bid 价尝试成交、有效时长 = bar 内。未定义限价位置不解释概率。
- **失败分**(model_kind=heuristic,model_version=heuristic-1.0):
  `p_fail = clamp(0.05, 0.95, 0.55×(1-spread) + 0.30×(100/(vol+100)) + 0.15×(1000/(oi+1000)))`
  - spread = clamp01(1 - 相对价差),相对价差 = (ask-bid)/mid(spread 大 → 1-spread 小 → 失败分高,方向已核实)
  - vol = 近端成交量,oi = 未平仓量;缺失/零值按 0 处理(分量=1,失败分高)
  - 权重 0.55/0.30/0.15 是待校准默认值,必须显式版本化
- **确定性**:伪随机按 `run_seed + symbol + bar_ts + contract + attempt_index` 派生(fnv 或 xxhash 混合),新增候选不改变已有订单结果;同 seed 同轨迹
- **seed 注入**:`backtest.OptionsData` 加 `RunSeed int64` 字段(默认 42 确定性;CLI `-seed` 覆盖,backtestexec.Options 透传);未显式提供时用默认 42
- **口径**:只对真正发出成交尝试的**卖出期权动作**(SellPut/SellCall)抽样;正股建议/HOLD/DATA_BLOCKED/候选淘汰不入分母;买入/平仓动作不做未成交模拟(保持现状确定性)
- **指标**:`unfilled_ratio = unfilled_count / attempt_count` + 双计数(`attempt_count`/`fill_count`/`unfilled_count` 同时输出);分母为 0 → `not_applicable`(null),不得报 0%
- **区分「模拟未成交」vs「人工未执行」**:Trade 加 `Filled bool`(false = 模拟未成交)+ `UnfilledModel string`(为空 = 非期权尝试/人工路径);人工未执行计数后续切片再补,本片只做数据位
- 未成交时:不入账(现金/持仓不变),但 Trade 留痕 `Filled:false`;后续 bar 的状态不得假设该单成交
- 未成交**不重复计罚**:已通过「没有该笔收益」进入净收益,结算路径不再额外扣罚

## 改动面

- `internal/backtest/state.go`: OptionsData 加 RunSeed;State 加未成交抽样状态(尝试计数)
- `internal/backtest/backtest.go`: settleOptionTrade(按 Bid 尝试成交时抽样 p_fail);Result 加未成交统计结构;Trade 加 Filled/UnfilledModel
- `internal/backtest/options_data.go`: 无(构造路径不动)
- `internal/backtestexec/backtestexec.go`: Options 加 Seed;Run 透传 OptionsData.RunSeed
- `cmd/wbot/main.go`: runBacktest 加 `-seed` flag
- 新包/文件:`internal/backtest/unfilled.go`(p_fail 计算 + seed 派生 + 常量,纯函数便于单测)

## Verify(验收)

- 同 seed 两次运行 trace 完全一致;不同 seed 未成交模式不同
- 高流动性(价差窄/量大)样例未成交率 < 低流动性样例(单测:构造两种 quote 断言)
- HOLD/DATA_BLOCKED 条目不进入 attempt_count(单测断言计数为 0)
- 缺 vol/oi 的候选:p_fail 不 panic、落在 [0.05,0.95]
- `-seed` 覆盖生效;默认 seed=42 下旧测试仍确定性通过
- gofmt/vet/test/race/staticcheck 全绿 + verify.sh

## Links

- 裁决书: ~/.claude/plans/mutable-nibbling-music.md §二
- 报告口径: doc/BACKTEST_REPORT.md §3
- 主任务记录: doc/tasks/2026-08-13-backtest-toolchain.md

## 评审结论(2026-08-13,reviewer a0db25a6d208ab7cf)

- **结论**: 有条件合入(修复 P1 后);功能类型 **feature**;API 向后兼容通过;CI 覆盖通过;密钥安全通过
- **[P1] attempt_index 用全局 st.AttemptCount 违反契约「新增候选不改变已有订单结果」**:任何早期新候选(不同 symbol/contract)把后续所有尝试序号 +1,改变既有订单 draw(已实测翻转)。修复:尝试序号按合约序号(`State.AttemptsByContract map[string]int64`,按 p.Code 自增),并补**双世界稳定性测试**(同 seed,加/减早期无关候选,断言既有订单 Trade/Filled 与未成交计数不变)
  - **已修复(2026-08-13)**:State 加 `AttemptsByContract`(惰性初始化),settleOptionTrade 按 p.Code 自增后作 attempt_index;attemptDraw 签名与派生输入不变。新增 `TestUnfilledDualWorldStability`:世界 A 仅 C105 单(bars 1/2/4),世界 B 在 bar 0 先插 X100 卖出尝试(seed 11,液态报价必成交);断言 C105 各单 Ts/Filled/UnfilledModel 与全局未成交计数两世界一致。已验证:回退全局计数时该测试复现翻转(C105 bar4 从成交变未成交),按合约计数后通过
- P2 后续(不阻塞合入): ① 正股 buy/sell 与 exercise/expire 的 Trade 显式 Filled:true ② doc/API.md + doc/BACKTEST.md 补 -seed/Result.Unfilled/Trade.Filled 文档 ③ SaveParams 持久化 seed ④ DailyOrders 按尝试计数(与 S1 删日上限耦合,寿命有限) ⑤ S4 处理 unfilled_model 对象形状映射(报告 §3 ↔ S3 扁平字段)⑥ CLI 汇总行加未成交指标
- P3: seed 0 语义注释精确化、储备校验顺序(抽样先于校验)、缺报价 0.95 已注释固定

## Next

- reviewer 复核 P1 修复(commit 后于 1ee47c2)→ 合入 main
- S4(报告数据面)依赖 S3 的 Result 未成交字段 + P2⑤ 映射观察
