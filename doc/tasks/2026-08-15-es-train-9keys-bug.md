# es_train 报告 candidates.params 丢弃 #88 新 4 参数(2026-08-15 #87 训练中发现)

## Goal

修复 `cmd/wbot/backtest_train.go` `tacticalParams()` 硬编码 9 键问题:搜索空间含 #88 新参数(profit_take_pct/put_delta_max/call_delta_max/min_iv_rank)时,es_train 报告 candidates[].params 与 boundary_hits 失真(仅显示 9 键,新 4 键被丢弃)。训练与执行本身已正确使用全键(identity.config.params 完整,轨迹 mask_reason 证实生效),本任务只修展示/边界检查。

## 修复点

- `tacticalParams()` 从硬编码 9 键(move_interval_pct..max_dte)改为按搜索空间(训练范围 JSON 的键)动态构建参数列表,新 4 键按与既有参数一致的规则派生(均为连续数值型:均匀采样 [lo,hi];整数型 min_dte/max_dte 保持离散步长)
- boundary_hits 检查覆盖全部搜索键
- 约束参数化(先 min_dte 再非负跨度)保持

## 约束

- worktree:.claude/worktrees/fix-es-train-9keys 分支 fix/es-train-9keys 基线 04b3aa8;verify.sh 全绿;署名按实际编写模型
- 兼容:旧 9 键搜索空间行为不变(无新键时输出同旧)
- 单测:断言候选 params 键集合 == 搜索空间键集合(13 键空间)、boundary_hits 覆盖新键、旧 9 键空间回归
- 不修其他问题;发现新问题只记录回报

## State

- [x] 派单
- [x] 实施 + verify.sh 全绿(2026-08-15 coder:cmd/wbot/backtest_train.go tacticalParams 改按搜索空间键动态构建 + boundaryHits 缺键防护;单测 3 个;verify.sh ok)
- [ ] reviewer 评审
- [ ] 合入主基线

## Links

- 发现:#87(doc/tasks/2026-08-15-retrain-00700-fullhistory.md「重要发现 2」;报告 bt-HK.00700-123-60dfeae2/7d29ffa1 candidates 仅 9 键)
- 参数:#88(doc/tasks/2026-08-15-return-boost-params.md)
