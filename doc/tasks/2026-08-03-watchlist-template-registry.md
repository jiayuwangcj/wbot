# 排期:watchlist 模板统一到 internal/strategy 注册表(去双源漂移)

## 状态

**✅ 已完成**(2026-08-03)。Step A(参数对齐)+ Step B(唯一来源化)均已落地。

## 问题(原始)

⑫-b(feat/strategy-impl)定义了策略模板的**唯一来源**是 `internal/strategy` 注册表(`Templates()`/`Factory()`/`Lookup()`,见 doc/BACKTEST.md)。但 `/v1/strategies` 端点与 watchlist 校验曾用 `internal/watchlist` **独立硬编码**的一份模板,与引擎注册表漂移(expiry_rule 枚举、lot_size、cash_reserve 缺失)。

## Step A(2026-08-03,#309):参数对齐

expiry_rule 加 `days`;cc/CSP 补 `lot_size`(100);CSP 补 `cash_reserve`(1.0);测试 pin 对齐断言。详见归档 `doc/tasks/2026-08-03-watchlist-template-parity.md`。

## Step B(#311):唯一来源化

1. **`internal/strategy` 承担 JSON 契约**:新增 `ContractTemplate`/`ContractParam`(json tag + choices/description 渲染)与 `ContractTemplates()`——引擎 string+Allowed 参数渲染为 `choice`+Choices,Help 渲染为 Description,Min/Max 留在引擎侧
2. **`strategy.Validate(name, params)`**:从 buildParams 拆出的公开校验面(未知模板/参数、类型、范围);Factory 复用同一 Lookup 路径
3. **`internal/watchlist` 删硬编码**:删 `Template`/`Param` 类型与 `Templates()`;`Validate` 变为薄委托——buy-hold 特判(引擎一等、无参数,错误文案保留「unknown parameter … for strategy」)+ 其余委托 `strategy.Validate`
4. **`/v1/strategies` 渲染**:httpapi 用 `[]ContractTemplate{buy-hold} + strategy.ContractTemplates()`,buy-hold 附加逻辑与注释移入 httpapi
5. **测试迁移**:契约 pin 从 watchlist 包迁到 `strategy/strategy_contract_test.go`(TestContractTemplates + TestValidate);watchlist/template_test.go 保留写面语义(委托 + buy-hold 特判);httpapi 契约测试改消费 `strategy.ContractTemplate`,错误文案断言适配新注册表文案(`unknown param`/`want a number` 等)

## 验证

- go test:strategy/watchlist/httpapi/cmd/webui 全绿
- 端到端:`/v1/strategies` 返回 buy-hold(无参数)+ cc 5 参数/CSP 6 参数,choice=[next_expiry, days](真实 PG)
- verify.sh 连跑两遍全绿;CI 五检查全绿
- JSON 契约不变形:params null(旧 nil)语义保留,buy-hold 仍居首位

## 收益

单一来源:引擎模板加参数/改枚举 → `/v1/strategies` 与写面校验自动跟随,不再双源漂移。引擎错误文案现为 CLI/API 共用(watchlist add 400 body 与 backtest 校验同一来源)。
