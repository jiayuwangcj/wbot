# S1 参数后端与兼容迁移

**State**: codex 已交付(提交 171480b,2026-08-13)→ 真 PG 集成测通过 → reviewer 评审中
**分支/worktree**: feat/s1-param-backend @ .claude/worktrees/s1-param-backend(main 基线 3fd2fba)

## Goal

wheel 参数面按裁决书(`~/.claude/plans/mutable-nibbling-music.md` 一、参数面新契约)重构:

- **战略参数(人工)**: `full_position_price`(满仓价,>0)、`zero_position_price`(清仓价,>满仓价)、`max_inventory`(最大持股数)——替代 `price_position_curve`(多点曲线)
- **战术参数(自动)**: `move_interval_pct`(再次出手价差,新)、`min_premium_per_share`(最低每股权利金,新)、`stock_switch_pct`(正股切换阈值,新)、`trade_gap`(免交易库存差,由 `no_trade_gap` 更名)、`min_option_quality`、`min_dte`/`max_dte`(键不变)
- **删除**: `max_daily_orders` / `extreme_max_daily_orders`(每日出手上限,人工决定)及全部领域逻辑
- **存量兼容(新读旧/只写新)**: ParseConfig 旧键→新键映射;多点曲线取端点=有损迁移(标 `migration_lossy=true`,保留原 JSON 审计值);旧限额键/lot_size 忽略但记迁移告警计数

## Constraints

- 新战术键(move_interval_pct/min_premium_per_share/stock_switch_pct)**必须可选**(存量 00883/09988 配置不含这些键,解析不得失败),默认值语义 = 关闭新行为:
  - `move_interval_pct` 默认 0 = 不限制出手间隔(0 表示任何价格变动都可出手)
  - `min_premium_per_share` 默认 0 = 不限制最低权利金
  - `stock_switch_pct` 默认 0 = 关闭正股切换机制(仅 `>0` 时启用,语义=距上次有效成交价变动 ≥ 该 % 后只产生正股建议)
- 曲线端点迁移:第一点 price → `full_position_price`,最后点 price → `zero_position_price`;`migration_lossy=true` 且保留原始 `price_position_curve` JSON 供审计。**满仓价语义 = 曲线第一点价格,清仓价 = 最后点价格**(第一点=最低价=满仓,最后点=最高价=清仓,2026-08-13 契约)
- 删 MaxDailyOrders/ExtremeMaxDailyOrders 领域逻辑后,wheel 决策**不再有任何每日次数上限**;报告中的每日提醒/成交次数统计保留(那是统计不是限制)
- `trade_gap` 沿用 `no_trade_gap` 语义:abs(目标-有效)<=值 → HOLD
- 单位语义:percent 一律小数(0.018 = 1.8%)还是整数(1.8)?**裁决:percent 参数用小数表示**(如 move_interval_pct=0.018 表示 1.8%),与既有 min_option_quality [0,1] 口径一致;DTO 文档展示为中文「%」时 ×100。CLI/JSON 输入必须统一,不得混用
- 每标的独立:参数集绑定 symbol+market+currency+config_version(现有 wheel_configs 版本机制已满足,无需新表)
- 前端同切修改:web/src/api/types.ts WheelParams、WheelForm(两锚点→满仓/清仓价)、watchlist Page/trace 显示、测试

## 改动面(影响文件)

- `internal/wheel/wheel.go`: Config struct、Validate、Evaluate(删每日上限检查)、决策规则
- `internal/strategy/strategy.go`: wheel 模板(wheel 模板 Param 列表)、ParseConfig(新键解析 + 旧键映射 + migration 审计)、buildParams
- `internal/wheelrun/runner.go`: wheelReviewRules(LLM 审核提示词参数名)
- `internal/httpapi/` 测试与契约
- 前端 `web/src/`: api/types.ts、WheelForm、watchlist Page/trace 组件及测试
- 文档: doc/API.md、doc/WHEEL_STRATEGY.md、doc/BACKTEST.md(参数表/示例去旧键,加新契约;报告 schema 见 doc/BACKTEST_REPORT.md 不在此片)

## Verify(验收)

- gofmt/vet/test/race/staticcheck 全绿 + 前端 vitest/build
- 新增测试:旧配置(price_position_curve 多点 + no_trade_gap + max_daily_orders + lot_size)→ 新 Config 字段正确、migration_lossy=true、原 JSON 保留;新键配置直读;full/zero_position_price 边界(<=0、zero<=full 拒绝)
- 存量 00883/09988 配置解析成功(旧键映射路径)
- wheel 决策不再受每日次数限制(原 max_daily_orders 相关测试改为断言无上限)

## Links

- 裁决书: ~/.claude/plans/mutable-nibbling-music.md
- 参数字典/映射表: 裁决书「一、参数面新契约」表(展示名/键/单位/约束/旧键/类别)
- 主任务记录: doc/tasks/2026-08-13-backtest-toolchain.md

## 评审结论(2026-08-13,reviewer aefe7e873c20a548f)

- **结论**: 有条件合入;功能类型 **feature**;达到可使用阶段;密钥/CI/提交粒度通过
- **[P1 合入前] strategy.go 模板与 ParseConfig 缺 max_quote_age_seconds**:S1 重写的 wheel 模板不含该键(实库配置已在用,US.JD 曾因 unknown param 停摆,见开发线 e542445);S1 从 3fd2fba 派生不含 e542445,合入时冲突风险吞修复。处理:按 e542445 同款补进 S1(模板默认 86400 Min 1 + ParseConfig 映射)
- P2 随 S2 收口: ① llmreview.go 提示词残留旧契约(字段说明「价格-目标库存曲线、每日订单数」、审核项 1/项 4 未同步)② doc/API.md:31 NEEDS_RECONFIGURATION 残留
- P3 观察: ① max_inventory 浮点存量行迁移硬失败(warning+取整)② 双键并存静默丢曲线不记告警 ③ 新战术参数接线(LastEffectiveFillPrice 调用方、StockSuggestion surfacing)④ backtestexec.SaveParams 与 httpapi 严格校验不一致(不可达)⑤ GET 读路径旧行展示迁移

## 验证补充(2026-08-13 主会话)

- 真 PG 集成测补跑通过:worktree 内 `go test -count=1 ./internal/httpapi/ -run Integration`(DSN 取自 ~/.wbot/serve.env,去引号导出)全绿;含 watchlist 版本化 wheel 历史、backtests_exec 等 8 项集成测

## P1 修复(2026-08-13,Claude coder,接 171480b 后单提交)

- **已修复**: wheel 模板补 `max_quote_age_seconds` 参数声明(Default 86400 / Min 1 / Max MaxFloat64,与 defaultMaxQuoteAgeSeconds 一致);ParseConfig 解析该键(asInt,错误带 `strategy wheel: param max_quote_age_seconds:` 前缀)并映射 `cfg.MaxQuoteAgeSeconds`——参照开发线 e542445 同款
- **键面核对**: S1 新模板其余键(full/zero_position_price、max_inventory、move_interval_pct、min_premium_per_share、stock_switch_pct、trade_gap、min_dte、max_dte、min_option_quality、strategic_state)与 ParseConfig 映射齐全;max_quote_age_seconds 是唯一缺失的保留键;max_daily_orders/extreme_max_daily_orders/no_trade_gap 按 S1 契约删除、lot_size 忽略,未加回
- **测试**: 新增 TestParseConfigMaxQuoteAgeSeconds(显式 3600 解析成功 / 缺省默认 86400 / 0、负数、非数字报错含键名);TestContractSchemaRequiredAndDefaults 与 TestParseConfigRequiresStrategicInputs 补默认值断言
- **自测**: gofmt/vet + `go test -count=1 ./internal/strategy/ ./internal/wheel/ ./internal/wheelrun/` 全绿

## Next

- → reviewer 复核 P1 补丁 → 合入开发主干线;S2 依赖 S1 合入后派单(P2-1 llmreview 并入 S2)
