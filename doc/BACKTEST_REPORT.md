# 回测报告数据面契约(schema_version 1.0,定稿 2026-08-13)

本文是回测/ES 训练报告 JSON 的**唯一事实源**。JSON 是单一事实源,HTML 与 Discord 只做确定性投影(Go html/template / embed 字段直出,**禁止 LLM 发挥**)。任何切片的轨迹/报告结构不得自行发明,一律复用本 schema。变更需更新 `schema_version` 并走评审。

关联:doc/BACKTEST.md(回测契约)、doc/tasks/2026-08-13-backtest-toolchain.md(任务与裁决)、doc/issues/draft-2026-08-13-backtest-rl-sol-eval.md(sol 设计评审)。

## 0. 顶层结构

```json
{
  "schema_version": "1.0",
  "report_id": "bt-{symbol}-{run_seed}-{short-hash}",
  "report_kind": "single_run | es_train",
  "identity": { ... },
  "train": { ... },
  "result": { ... },
  "generations": [ ... ],
  "candidates": [ ... ],
  "audit": { ... },
  "risk": [ ... ],
  "trajectory": [ ... ]
}
```

- `report_id`:由 symbol + run_seed + 输入哈希前 8 位确定性生成;**重复执行同 ID 幂等**(覆盖或拒绝写)。
- 所有数值金额字段同时给 `amount`(币种)与 `return_pct`(收益率,小数,如 0.0123 = 1.23%);百分比一律小数表示,不接受 `0.05`/`5` 两种含义并存。
- 时间一律 RFC3339 UTC `Z`。

## 1. identity(身份,所有报告必填)

```json
{
  "symbol": "HK.00883",
  "market": "HK",
  "currency": "HKD",
  "config_version": 3,
  "code_version": "git-sha-12",
  "data_window": { "from": "2024-01-01T00:00:00Z", "to": "2025-06-30T00:00:00Z" },
  "windows": {
    "train": { "from": "...", "to": "..." },
    "valid": { "from": "...", "to": "..." },
    "test":  { "from": "...", "to": "..." }
  },
  "capability_status": "RESEARCH_ONLY | DATA_BLOCKED | READY",
  "blocked_by": null,
  "run_seed": 42,
  "config": { "params": { "...": "人工给定战略参数与(单次回测时)完整参数" } }
}
```

- `windows` 仅 `es_train` 必填;`single_run` 时 `data_window` 即回测区间。
- `capability_status` 语义沿用 doc/BACKTEST.md;当前事件数据不足,所有训练产物 `RESEARCH_ONLY`。
- `config.params` 记录**脱敏后的完整输入参数**(战略参数人工值 + 战术参数取值),供复现。

## 2. train(训练信息,`es_train` 必填)

```json
{
  "algorithm": "ES",
  "algorithm_version": "es-1.0",
  "generation_count": 25,
  "population_size": 20,
  "evaluation_count": 500,
  "seeds": [42, 43, 44, 45, 46],
  "stop_reason": "early_stop | max_generations | budget_exhausted | timeout",
  "stop_detail": "验证集最优 8 代无 0.5% 以上相对改善",
  "duration_sec": 312.5,
  "evaluation_estimate": "启动前输出:预计 20×40=800 次回测"
}
```

- `generation_count`、`population_size`、`evaluation_count` 三者分开计数,不得混称「迭代次数」。

## 3. result(结果,所有报告必填;`es_train` 报告样本外)

```json
{
  "net_return_pct": 0.0123,
  "net_return_amount": 123.45,
  "baseline_return_pct": 0.008,
  "baseline_name": "buy-hold",
  "excess_return_pct": 0.0043,
  "max_drawdown_pct": 0.05,
  "tail_loss_pct": 0.012,
  "attempt_count": 120,
  "fill_count": 96,
  "unfilled_count": 24,
  "unfilled_ratio": 0.2,
  "unfilled_model": {
    "model_kind": "heuristic",
    "model_version": "heuristic-1.0",
    "order_assumption": "卖出期权,按 Bid 价尝试成交,有效时长=bar 内",
    "components": {
      "spread_weight": 0.55,
      "volume_weight": 0.30,
      "oi_weight": 0.15
    }
  },
  "cost_model": {
    "fees_included": true,
    "slippage_included": false,
    "taxes_included": false,
    "assignment_included": false,
    "description": "费用计入;滑点/税费/指派模型未接入"
  },
  "manual_not_executed_count": 0,
  "hard_violations": 0,
  "stock_assignment_rate": 0.0
}
```

口径(与 doc/tasks/2026-08-13-backtest-toolchain.md 一致):

- `unfilled_ratio = unfilled_count / attempt_count`;`attempt_count` **只计真正发出成交尝试的期权动作**;正股建议/HOLD/DATA_BLOCKED/候选淘汰不入分母;分母为 0 时 `unfilled_ratio = null`(返回 `not_applicable`,不得报 0%)。
- `fill_count` 可由 `attempt_count - unfilled_count` 推导,但两者同时输出计数,防舍入/口径不一致。
- `manual_not_executed_count`:模拟成交但人工未执行的条数(与模拟未成交区分)。
- `hard_violations`:最大库存违规/裸 Call/资金不足/未来泄漏计数;>0 时报告必须显著警示。

## 4. generations(逐代轨迹,`es_train` 必填)

```json
{
  "generation": 0,
  "evaluation_count": 20,
  "train_best_return_pct": 0.011,
  "train_mean_return_pct": 0.006,
  "train_median_return_pct": 0.007,
  "train_std_return_pct": 0.004,
  "history_best_return_pct": 0.011,
  "valid_best_return_pct": 0.009,
  "max_drawdown_pct": 0.04,
  "unfilled_ratio": 0.22,
  "effective_trades": 15,
  "population_dispersion": 0.31,
  "mutation_scale": 0.12,
  "duration_sec": 8.2
}
```

- 收敛图至少画「训练集当代最优、验证集最优、基线」三条线;未成交率与最大回撤独立呈现,不与收益共用尺度。

## 5. candidates(候选前 3–5 组,`es_train` 必填)

```json
{
  "rank": 1,
  "params": {
    "move_interval_pct": 1.8,
    "min_premium_per_share": 0.42,
    "stock_switch_pct": 6.5,
    "trade_gap": 60,
    "min_option_quality": 0.62,
    "min_dte": 5,
    "max_dte": 8
  },
  "stats": {
    "median_return_pct": 0.011,
    "p10_return_pct": 0.004,
    "p90_return_pct": 0.018,
    "median_max_drawdown_pct": 0.045,
    "median_unfilled_ratio": 0.2
  },
  "boundary_hits": { "move_interval_pct": false, "min_premium_per_share": true },
  "vs_baseline_pct": 0.0043
}
```

- 多 seed 给出中位数/P10/P90,不输出单点「最优」;`boundary_hits` 撞边界需显式标记(通常表示范围不合理)。
- 战略参数(满仓/清仓/最大持股)不在 `params` 中——人工自定,不参与搜索。

## 6. audit(审计明细,所有报告必填)

```json
{
  "input_snapshot_hash": "sha256-...",
  "params_dictionary_version": "params-1.0",
  "reward": {
    "function_version": "reward-1.0",
    "weights": { "lambda_dd": 0.3, "lambda_tail": 0.15, "lambda_turnover": 0.1 },
    "hard_failure_handling": "不软惩罚,直接失败;未成交已含于净收益,不重复计罚"
  },
  "search_space": {
    "move_interval_pct": { "min": 0.5, "max": 3.0, "unit": "%", "hit_boundary": false },
    "min_premium_per_share": { "min": 0, "max": 1.0, "unit": "HKD/股", "hit_boundary": true },
    "stock_switch_pct": { "min": 3.0, "max": 15.0, "unit": "%", "hit_boundary": false },
    "trade_gap": { "min": 0, "max": 200, "unit": "股", "hit_boundary": false },
    "min_option_quality": { "min": 0.5, "max": 0.8, "unit": "[0,1]", "hit_boundary": false },
    "min_dte": { "min": 5, "max": 10, "unit": "自然日", "hit_boundary": false },
    "max_dte": { "min": 5, "max": 14, "unit": "自然日", "hit_boundary": false }
  },
  "strategy_params_snapshot": { "migration_lossy": false, "original_json": null }
}
```

- `migration_lossy=true` 时 `original_json` 保留旧配置原值(审计),报告身份处同时标注。
- 所有单位为无歧义展示名(中文)+ 机器键;币种/股/自然日/% 不得省略。

## 7. risk(风险提示,所有报告必填)

```json
[
  "RESEARCH_ONLY:历史事件数据未解锁,本结果只用于研究,不驱动提醒",
  "DATA_BLOCKED:成交/指派/人工处置事实缺失,未成交率为启发式估算",
  "bar-time replay:非事件级回放,不含逐 quote 成交时序"
]
```

- Discord 摘要必须带状态,不只展示收益。

## 8. trajectory(RL-ready 轨迹数据面,`es_train`/`single_run` 可含,冻结供阶段 C)

每步决策一条,版本化落盘,与报告同生命周期:

```json
{
  "step": 0,
  "decision_time": "2024-01-05T02:00:00Z",
  "bar_ts": "2024-01-05T02:00:00Z",
  "state_before": {
    "underlying_price": 52.1,
    "actual_inventory": 300,
    "effective_inventory": 310.5,
    "target_inventory": 350,
    "cash": 50000,
    "strategy_state": "NORMAL",
    "last_filled_price": 51.8,
    "bars_since_last_action": 3
  },
  "candidates": [
    {
      "contract": "HK.00883 2024-01-12 50.0 P",
      "is_call": false,
      "expiry": "2024-01-12",
      "strike": 50.0,
      "delta": -0.35,
      "bid": 0.62,
      "ask": 0.7,
      "spread_pct": 0.114,
      "implied_vol": 0.28,
      "theta": 0.02,
      "volume": 1200,
      "open_interest": 5000,
      "lot_size": 100,
      "quality": 0.78,
      "masked": false,
      "mask_reason": null,
      "raw_score": null
    }
  ],
  "action": { "type": "SELL_PUT | HOLD", "candidate_index": 0, "quantity": 1 },
  "fill": { "simulated": true, "filled": false, "unfilled_model_version": "heuristic-1.0" },
  "state_after": { "effective_inventory": 310.5, "cash": 50000 },
  "reward_atoms": {
    "equity_delta": 0.0,
    "fees": 0.0,
    "slippage": 0.0,
    "incremental_drawdown": 0.0,
    "tail_exposure_delta": 0.0
  },
  "termination": null,
  "versions": {
    "config": 3,
    "data_hash": "sha256-...",
    "code": "git-sha-12",
    "fill_model": "heuristic-1.0"
  }
}
```

- **冻结约束**:决策特征只用 `event_time <= decision_time` 数据;候选被选/未选都留 `masked/mask_reason`;`raw_score` 留未 mask 原始分(供审计模型是否偏好被风控拒绝动作)。
- 深度 RL 阶段 C 只消费本数据面,任何新增字段需版本化扩展。

## 9. HTML/Discord 投影约束

- HTML 由 Go `html/template` 渲染本 JSON(或内存同构结构),禁止大模型生成 HTML;首屏 = 状态/样本外收益/回撤/未成交率/停止原因,明细折叠卡片;iPhone 16 Pro Max 430px 验收,同时覆盖 390px,正文不得横向滚动。
- Discord embed 只推 5–7 核心字段 + 状态 + 报告链接;字段超限时**不截断关键风险状态**。
- 同一 JSON 重复渲染必须一致;关键汇总可由明细复算(验收脚本对账)。

## 10. 版本与演进

### `es_train` 构造约定（S5）

S5 在不改动 `single_run` 字段的前提下启用顶层 `train`、`generations`、`candidates` 与 `trajectory`。逐代收益字段保持原始净收益率口径；ES 选择使用版本化奖励 score，原子净收益、回撤、尾损和成交成本权重保存在 `audit.reward`，样本外原始收益分布保存在 `candidates.stats`。`identity.windows` 使用连续、无重叠的时间顺序窗口，`train.seeds` 顺序为训练、验证和至少 5 个测试 seed，训练 seed 不得与测试 seed 相同。

搜索审计只允许七个战术键（DTE 两键由一个“最短值 + 非负跨度”约束维表达），战略键不得出现在 `candidates.params`。`audit.search_space` 同时保留范围、中文单位和撞边界标记；没有候选在全部封存 seed 稳定超过基线时，`candidates` 为空，表示“无可推荐参数”，不得回写线上配置。

- `schema_version` 变更 = 破坏性契约变更,走评审 + 双版本兼容期(读旧写新)。
- 所有报告产物带 `report_id`;`-push`/`-cache` 为显式动作,重复执行同 ID 幂等。
