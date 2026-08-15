# 任务:批量自动寻优 -tune 模式(#72)

## 派单(2026-08-14 主会话)

- 前置 #71 已合入(主基线 4d60891),本分支基于 4d60891
- **实施:coder(Claude)直接实施**——2026-08-14 老板指令「luna 已用尽,本次内容完成后不再使用,直接用 claude 开发」;codex 不再派
- 署名:Co-Authored-By: Claude <noreply@anthropic.com>

## Goal

- CLI `-tune` 模式:一次命令跑 多搜索空间 × 多种子 自动寻优(区别于 -train 单空间单种子)
- 输入例:`backtest -tune '{"spaces":[{...},{...}],"seeds":[42,7,...]}' -report -push`
- 流程:逐组(空间×种子)跑 ES(组内并行,复用 #70 8-worker ParallelMap);每组输出 reward/收益/回撤/未成交率/耗时/收敛代数
- 自动剪枝(racing,老板指令「明显不可能的方向就不用再试了」):前 3-5 代观察趋势,历史最优低于基线(买入持有)或显著落后(如 < max(基线, 全局当前最优×0.5))即终止;窗口/阈值可配置,默认值文档化
- 全局最优:reward 排序 + 样本外多种子稳定性(复用既有候选推荐逻辑)
- 最终报告:最优参数全窗报告(含 #71 initial_cash/年化/损耗/仓位占比),`-push` 推送
- 确定性铁律:同输入(空间/种子/数据/剪枝参数)→ 同输出(复用 #70 有序并行收集,逐位一致)

## Constraints

- 报告 schema 保持 #71 的 1.2,不引入新版本
- 中间训练报告不推送(仅调试);`-push` 只推最终报告
- 输出:① 寻优汇总表(每空间×种子一行,stdout/日志)② 最优参数最终报告(交付物)
- verify.sh 全绿;无 realtime serve 回归

## Links

- #70 并行训练:internal/backtestes ParallelMap(有序收集确定性)
- #71 训练模式:cmd/wbot/backtest_train.go(报告 schema 1.2、样本外多种子协议)
- doc/BACKTEST.md「多空间自动寻优(-tune)」契约

## State

- [x] #71 合入后实施(-tune 模式完整实现,2026-08-14)
- [x] 多空间×多种子寻优 + 全局最优(样本外中位 reward 排序,同分先出现)
- [x] racing 剪枝(逐代 hook、窗口/阈值可配、好组不误杀、无望窗口兜底取最不差)
- [x] 最终报告 + `-push` 只推最终;中间组不产生报告
- [x] 测试 + scripts/verify.sh 全绿

## Next

评审(判定 feature/bugfix)→ 合入 → 真实 PG 实测:00700 批量寻优(如 4 空间 × 3 种子 = 12 组,~1 分钟内),推送最优参数报告;对账剪枝效果(12 组墙钟)。

---

## 实施摘要(2026-08-14,coder 收口)

### 改动文件

- `internal/backtestes/es.go`:Config 新增 `PruneCheck` 逐代 hook + `PruneProgress`(Generation/HistoryBestScore/BestScore/EvaluationCount/PrunedReason);hook 在早停判定**之前**调用,false → `StopReason="pruned"`(reason 落 StopDetail);HistoryBestScore = 历史最优 train reward,全局最优账本完整(每代都调)
- `cmd/wbot/backtest_tune.go`(新):-tune 全流程
- `cmd/wbot/backtest_train.go`(重构,行为不变):train/tune 共用 `sampleOutTestCandidates`/`reportCandidatesList`/包级常量与 testedCandidate
- `cmd/wbot/main.go`:-tune/-tune-prune/-tune-prune-window/-tune-prune-factor flag;与 -train 互斥(独立前置检查);校验坏 spec 在连 DB 前报错
- `doc/BACKTEST.md`:-tune 契约(flag 表、spec 形式、执行序/确定性、剪枝语义与默认值、汇总表格式、全局最优选择、最终报告口径)

### 关键设计

- **剪枝判定**(纯函数 `tuneShouldPrune`):`gen+1 < window` 或 `globalBest < baseline` → 不剪;否则 `floor = max(baseline, globalBest*factor)`,`historyBest < floor` → 剪
- **确定性**:组按 spec 顺序串行;hook 在搜索 goroutine 内同步;ParallelMap 有序收集 → 同输入同输出;仅墙钟字段(dur_s/报告 duration_sec)除外(与 -train 现状一致)
- **好组不误杀**:hook 先用自己的 HistoryBestScore 更新全局最优再比较,当前最优组不可能被相对条款剪掉
- **无望窗口兜底**:全局最优仍低于基线 → 全不剪,跑完取最不差组当交付物
- **预算转移**:被剪组消耗更少 eval,墙钟提速;每组独立预算(默认 840)
- **报告口径**:最终报告为 es_train 类 BuildES 报告(Run=全窗重跑最优参数,年化/损耗/仓位占比全窗口径);Candidates=全组可推荐候选按样本外中位 reward 全局排序;Train=获胜组元数据
- **-cache 与 -tune 互斥**:-cache 的 sampleOutPassed 用 test 窗基线比候选 P10,会混 test 窗与全窗口径

### 汇总表格式(契约,stdout)

```
tune_start spaces=N seeds=N groups=N budget=<每组预算> windows=<start>..<train>|<valid>|<test>
tune_group space=N seed=N status=ok|pruned reward=<train历史最优reward> sample_out=<样本外中位reward> ret_pct=.. dd_pct=.. unfilled_pct=.. gens=.. converged=.. evals=.. dur_s=.. pruned_at=..
tune_best space=N seed=N median_reward=.. report=<报告ID> duration_sec=..
```

## 测试与验证(2026-08-14)

- `internal/backtestes/es_prune_test.go`:剪枝 hook 终止+reason 落 StopDetail;keep 时每代调用、HistoryBestScore 为运行最大值、EvaluationCount 单调、仍可早停
- `cmd/wbot/backtest_tune_test.go`:spec 解析(合法/空 spaces/空 seeds/重复 seed/0≡42/战略参数拒绝/区间倒置);`tuneShouldPrune` 六场景(窗口内/全局低于基线/低于基线/显著落后/最优不误杀/阈值内);`tunePruneCheck` 串行三组时序;`bestTuneGroup` 最高中位+同分先出现;收敛代数;历史最优;汇总行 pruned 标记;CLI flag 校验(与 -train 互斥、window≥1、factor∈[0,1]、坏 spec 连 DB 前报错)
- `scripts/verify.sh` 全绿(verify: ok):gofmt / go test ./... / vet / race(相关包) / staticcheck / 交叉编译 / CLI smoke / accept 脚本
- 实测样例(真实 PG):
  ```bash
  wbot backtest -dsn "$WBOT_PG_DSN" -symbol HK.00700 -strategy wheel \
    -params '{"full_position_price":48,"zero_position_price":55,"max_inventory":22000}' \
    -tune '{"spaces":[{"move_interval_pct":["0.005","0.03"]},{"move_interval_pct":["0.005","0.03"],"min_option_profit":[100,300]}],"seeds":[42,7]}' \
    -report -push
  ```
