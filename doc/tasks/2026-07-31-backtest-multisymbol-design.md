# v2 下一刀：多 symbol 时间对齐（设计待拍板）

- **id**: `2026-07-31-backtest-multisymbol-design`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

ROADMAP v2「时间对齐」的拆解设计：多 symbol 组合回测的时间轴对齐语义。**blocked**——对齐方式、缺价处理、权重与再平衡周期存在多种合理设计，需用户/讨论拍板后实施。

## 设计要点（拍板内容）

1. **时间轴对齐**：union（所有 symbol 的 ts 并集）vs intersection（交集）vs 主 symbol 时间轴？
2. **缺价处理**：某 symbol 在时间轴上无 bar 时——跳过（保持仓位不估值？）、前向填充（复用最近收盘）、还是标记缺失？
3. **权重与再平衡**：等权静态？按市值？再平衡周期（每 N bar / 固定日）？
4. **组合 equity**：各 symbol 独立仓位 + 现金池；估值按对齐后的价格。
5. **CLI 形态**：`-symbols A,B,C`（逗号分隔，-dsn 输入）？

## Constraints

- 第一刀建议取最小语义：等权、不自动再平衡（静态权重）、intersection 对齐、无 bar 的 symbol 当日按最近价估值——若拍板采用，可立即实施。
- 不改单 symbol 行为；`internal/backtest` 现有 `Run` 不动（组合层新建）。

## Links

- [[ROADMAP]] v2；草稿：`doc/issues/draft-2026-07-31-backtest-skeleton.md`（非目标列「多 symbol 组合与时间对齐（后续刀）」）
- 前置：`doc/tasks/2026-07-31-doc-backtest.md`（已完成）
- Driven-By: /loop 循环主会话按计划优先级 ④ 拆出

## State

- **status**: `blocked`
- **last step**: 拆解完成；缺「对齐语义拍板」（用户或 discussion 确认）。
- **缺什么**: 上述设计要点 1-4 的确认（或直接采用「Constraints」里的最小语义）。

## Next

- 拍板后：组合层设计（多 symbol 加载 → 对齐 → 估值）→ 实施第一刀 → 测试 → CI。
