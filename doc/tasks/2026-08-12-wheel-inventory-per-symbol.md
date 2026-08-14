# 2026-08-12 wheel 库存按标的隔离(多标的模拟盘触发修复)

- **id**: `2026-08-12-wheel-inventory-per-symbol`
- **created**: `2026-08-12`
- **parent**: 老板指令 2026-08-12(JD 加入 watchlist 跑美股模拟盘 + 阶梯曲线参数;检查 wheel 能否触发卖 PUT)

## Goal

wheel runner 评估**每个 watchlist 标的**时,库存(actual_inventory / effective_inventory)只统计**该标的自身**的持仓,不再把整个模拟账户的股票持仓算进每个标的。修复后多标的模拟盘才能各自触发方向正确的订单。

## 现状(已探明,2026-08-12 实测)

- 模拟账户 acc 1907141 持有 5 只港股股票共 **46,200 股**(HK.00700×200、HK.00883×22000、HK.09988×1000、HK.01810×5000、HK.01766×18000;经 GET /v1/futu/account 实测)
- `internal/wheelrun/positions.go` PositionsInput 把**全账户**股票持仓累加进 stockShares(不按 symbol 过滤);`cmd/wbot/wheel_scheduler.go` futuPositions.Positions 返回全账户持仓
- 后果(实测 wheel_signals):
  - US.JD signal 425: target=1001(阶梯曲线 @31.24),actual=46200 → **gap=-45199** → 方向变成**卖 CALL** → 候选全部不过校验 → HOLD(本应 gap=+1001 → 卖 PUT 触发)
  - HK.00700 signal 422: target=390(@461.6),actual=46200 → gap=-45810 → 卖 CALL(本应 200 股自有持仓 → gap=+190 → 卖 PUT 触发)
- **直购现货(wheel 卖 PUT 之外)不存在**:wheel.Evaluate 只有 PUT/CALL/HOLD 三方向,候选全部来自期权报价,无股票买入路径(已确认,如实告知老板)

## 设计(拟)

1. **按 symbol 过滤持仓**:runSymbol 内把 Positions 结果按「该 position 属于被评估 symbol」过滤后再进 PositionsInput:
   - **股票持仓**:position 的 market 限定 symbol(qualifySymbol 结果)与被评估 symbol 完全相等才计入 stockShares
   - **期权持仓**:position 的合约 code 必须命中该 symbol 的 OptionChain 合约集合(chain contractSymbols)才计入 opts(chain 已在 runSymbol 中取得,无需额外请求);无法归属的期权持仓跳过并日志记录
2. PositionsInput 保持纯函数不变(过滤在 runner 层做,或在 PositionsInput 前加一步 filterPositions(symbol, positions, chainCodes));保持单元测试覆盖
3. 行为影响:单标的账户(现状 00700 场景)库存从全账户 46,200 变为自有 200 股 → 00700 触发逻辑变化(更正确);JD 库存 0 → gap=+1001 → 卖 PUT 1 张(受 max_daily_orders=1 限制)
4. 与 session-subscription(排队中)文件重叠面:internal/wheelrun/runner.go —— 本任务先行,后续任务在其上派发

## Constraints

- 不改变 wheel.Evaluate/Validate 决策逻辑本身;只修库存输入
- 模拟盘验证规模约束(max_inventory/quantity 校验)不变;verify.sh 全绿
- 独立 worktree + 分支 fix/wheel-inventory-per-symbol;署名按实际编写模型(codex 署 gpt-5.6-luna)
- 与 push-ui 工作树(discord_scheduler.go/internal/discord)无文件重叠

## Links

- 信号库:wheel_signals(symbol/current_price/target_inventory/inventory_gap 实测列)
- 账户持仓:GET /v1/futu/account(env=simulate,acc_id=1907141)
- 任务排期:#29(00700 实测配置)、#37(LLM 策略定时运行,排队)

## State

- **status**: `merged`(2026-08-12 评审通过合入)
- **last step**: reviewer 建议合入(bugfix,无 P0/P1)→ 合入 feat/llm-signal-endpoint(faa4f3d)→ 部署 serve 实测
- **评审 P2 改进项(排期)**:①无法归属期权分级日志(同标的未命中 chain → 显眼警告)②errUnsupportedOption 行为从 fail-closed 改 fail-soft,收口时已注明(本记录即为固化)③补空 chain/errUnsupportedOption 单测

## Next

- codex 实现 → verify.sh → reviewer 评审(功能类型判定 bugfix)→ 合入 feat/llm-signal-endpoint → 部署 serve → 实测 US.JD 卖 PUT 触发(LLM 审核 + Discord 卡片按钮确认)
- 收口后:JD 阶梯曲线(30.25→1100/31.25→1000/…/35.25→600)在现价 31.24 处 target=1001,触发条件成立
