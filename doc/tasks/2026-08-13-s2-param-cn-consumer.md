# S2 参数消费端中文化

**State**: ✅ 已合入开发主干线(merge b6a7ab5,2026-08-13;评审通过:bugfix,无 P0/P1)

## 评审结论(2026-08-13,reviewer a9e7da154b96cc319)

- **结论**: 合入;功能类型 **bugfix**;达到可使用阶段;P0/P1 无
- 逐项验证:迁移审计字段名与 S1 实现一致(4 字段,watchlist_test 实测)、「旧行自动映射」表述准确(normalizeLegacyParams 读路径)、NEEDS_RECONFIGURATION 已废弃准确(唯一发射方 runner.go 只写 READY/DATA_BLOCKED)、提示词语义完整(资金/库存/备兑/指派四类校验本体保留)、前端无破坏
- P3 观察(排期): ① doc/API.md:31 审计字段列全 4 个(补 migration_warning_count)② 「已废弃」补「值仍兼容旧行」一句 ③ llmreview 提示词统一「库存/持股数」术语 ④ 提示词文本无测试断言(可加必含字段断言测试)
**分支/worktree**: feat/s2-param-cn-consumer @ .claude/worktrees/s2-param-cn-consumer(基线 26445c4 开发线)
**执行**: Claude coder(S2 为文档/提示词/核对为主的轻改动,不派 codex)

## 交付记录(2026-08-13,Claude coder b6a7ab5)

- 改动 2 文件:llmreview.go 提示词 3 处(行 23/31/34,仅术语更新,审核语义完整);doc/API.md:31 NEEDS_RECONFIGURATION → 「已废弃」说明 + 迁移行审计字段展示(migration_lossy/migration_warnings/migration_original_price_position_curve)
- grep 核对:llmreview.go 旧词 0 命中;API.md 唯一命中为「已废弃」说明本身;CLI 帮助/前端/API 文档无 max_daily_orders/no_trade_gap/price_position_curve 残留;cmd/wbot 无需改
- P3⑤ 只读核对:前端无 NEEDS_RECONFIGURATION/旧字段引用;GET /v1/watchlist 已带迁移标记(CanonicalParams 经 wheel.Config JSON tag 持久化 migration_lossy 等 4 字段),无新 UI 需求
- 保留项(合理):strategy.go 迁移解析、watchlist.go StatusNeedsReconfigure 白名单(无生产写入方,P3 观察可评估清理)、trace.ts 历史读取兼容、WHEEL_STRATEGY.md:97 迁移说明
- verify:gofmt/vet + llmreview 测试通过;verify.sh 全绿(staticcheck 首次缺失,go install 后重跑全绿)

## Goal

S1 参数面新契约(满仓/清仓价、最大持股、删每日上限)合入后,把所有**消费端**的旧契约残留收口,保证 界面/CLI 帮助/LLM 审核文本/文档 四处口径一致。已核实:S1 已清理 wheelrun/runner.go、doc/WHEEL_STRATEGY.md、doc/BACKTEST.md、前端 WheelForm/types;剩余残留如下。

## 残留清单(已逐一核实)

1. **internal/llmreview/llmreview.go 提示词**(评审 P2-1,3 处):
   - 行 23 strategy_config 字段说明:「价格-目标库存曲线、最大库存、DTE 区间、报价质量、每日订单数和战略状态」→ 改新契约表述:满仓价格/清仓价格/最大持股数、DTE 区间、报价质量、战略状态(**删「每日订单数」**;曲线→两点锚点)
   - 行 31 审核项 1 方向反转:「…和价格-目标库存曲线一致」→ 改「…和满仓/清仓价格锚点一致」(或等价新语义)
   - 行 34 审核项 4:「…和 extreme 限制均不得超限」→ **删 extreme 限制引用**(extreme_max_daily_orders 已删,无每日上限;保留资金/库存/备兑/指派校验本体)
2. **doc/API.md:31 NEEDS_RECONFIGURATION**(评审 P2-2):迁移行不再存在该状态——S1 有损迁移后旧行自动映射(新读旧);删该状态值或改写为「迁移行(migration_lossy=true)展示」说明。同步核对 doc/API.md 是否有其他旧契约表述(已 grep:无「价格-目标库存/每日订单」残留)
3. **CLI 帮助核对**:wheel 参数帮助文本来自 strategy.go 模板 Param.Help(S1 已中文化);核对 `wbot backtest -params` / 配置错误文案无旧键名残留;`max_daily_orders`/`no_trade_gap`/`price_position_curve` 不应再出现在任何用户可见文案
4. **watchlist 展示迁移**(评审 P3⑤,只读):GET 读路径旧行(price_position_curve 存量,迁移后 migration_lossy=true)前端如何展示——检查 web/src 是否引用旧字段名或 NEEDS_RECONFIGURATION(已 grep 前端无 NEEDS_RECONFIGURATION 引用);如 GET 响应带迁移标记,确认展示为「已迁移(有损)」提示即可,不做新 UI

## Constraints

- **不碰 runBacktest 输出逻辑**(S4 独占 cmd/wbot/main.go 报告输出区);本片只做提示词/文档/展示文案
- 提示词改动须保持 LLM 审核语义完整:删每日上限后,资金/库存/备兑覆盖/指派风险校验本体不动,只更新术语
- 无前端新功能;如 P3⑤ 核对后发现前端需要展示迁移标记,最小改动(只读展示)
- 验收:gofmt/vet/test 全绿(重点 internal/llmreview 测试若断言提示词文案,同步更新)+ verify.sh

## 改动面

- `internal/llmreview/llmreview.go`(提示词 3 处)
- `doc/API.md`(:31 NEEDS_RECONFIGURATION)
- `cmd/wbot/main.go`(仅帮助文案核对,如有残留才改;不碰 runBacktest 逻辑)
- `web/src/`(仅 P3⑤ 核对后的最小展示改动,如有)

## Verify

- grep 全仓确认「价格-目标库存曲线」「每日订单数」「extreme 限制」「NEEDS_RECONFIGURATION」不再出现于用户可见文案(测试/历史文档除外)
- internal/llmreview 测试通过(如断言提示词,同步)
- gofmt/vet/test/race/staticcheck + verify.sh 全绿;独立分支提交

## Links

- 主任务记录: doc/tasks/2026-08-13-backtest-toolchain.md
- S1 任务记录: doc/tasks/2026-08-13-s1-param-backend.md(评审 P2/P3 清单来源)
- 参数字典: 裁决书 `~/.claude/plans/mutable-nibbling-music.md` 一、参数面新契约
