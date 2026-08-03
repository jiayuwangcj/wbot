# 排期:watchlist 模板统一到 internal/strategy 注册表(去双源漂移)

## 状态

**已登记,未排期**(2026-08-03)。Step A(参数对齐)已落地——见下方「已做」。

## 问题

⑫-b(feat/strategy-impl)定义了策略模板的**唯一来源**是 `internal/strategy` 注册表(`Templates()`/`Factory()`/`Lookup()`,见 doc/BACKTEST.md)。但 `/v1/strategies` 端点与 watchlist 校验仍用 `internal/watchlist` **独立硬编码**的一份模板(watchlist.go `Templates()`),历史注释「待 ⑫-b 合入后接入注册表」至今未兑现。

双源漂移(2026-08-03 实测对比):

| 差异 | `watchlist.Templates()`(HTTP 契约) | `strategy.templates`(引擎) |
| --- | --- | --- |
| `expiry_rule` 取值 | 修复前仅 `next_expiry` | `next_expiry` + `days` |
| `lot_size` 参数 | 修复前缺失 | 有(默认 100) |
| `cash_reserve`(仅 CSP) | 修复前缺失 | 有(默认 1.0) |
| `buy-hold` | 有(引擎一等策略,backtestexec 直接支持) | 注册表无(刻意) |

漂移后果:Web 表单无法配置引擎支持的参数(days/lot_size/cash_reserve),表单校验与引擎 Factory 约束不一致。

## 已做(Step A,2026-08-03,#309)

参数**对齐**(不动架构):expiry_rule 加 `days`;cc/CSP 补 `lot_size`(100);CSP 补 `cash_reserve`(1.0);watchlist.go 注释与 API.md 表述更新;测试补对齐断言(含 cash_reserve 仅 CSP 用例)。验证:`/v1/strategies` 返回新字段,表单自动渲染,引擎 Factory 校验同参通过。

## 待做(Step B,排期)

1. `strategy.Param`/`Template` 增加 JSON 契约能力(json tag + choices 渲染)或新增契约转换层
2. `/v1/strategies` 改从注册表渲染(buy-hold 特例处理:注册表外一等策略,或注册表登记 + Factory 特判)
3. `watchlist.Validate` 改调 `strategy` 校验(保留 buy-hold 空参数路径)
4. 删除 `watchlist.Templates()` 硬编码;全链测试(契约/CLI/backtest 集成)更新
5. 涉及 UI 表单渲染确认(choice 枚举来自 /v1/strategies,自动生效)
