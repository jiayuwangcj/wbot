# 2026-08-03: watchlist 模板契约与引擎注册表对齐(Step A,#309)

## 来源

Round 32 triage:三候选(ingest -from/-to、Provider 抽象、外部 cron 文档)实测核实**全部已实现**;排期项全待拍板;端点/验收/小程序/README 对账干净。TODO 扫描命中 `watchlist.go:26`「待 ⑫-b 合入后接入 internal/strategy 注册表」——⑫-b 已合入(#69)但从未接入,核实为**诚实欠账**。

## 漂移实测

| 差异 | watchlist.Templates()(HTTP 契约) | strategy.templates(引擎) |
| --- | --- | --- |
| expiry_rule 取值 | 仅 next_expiry | next_expiry + days |
| lot_size | 缺失 | 有(100) |
| cash_reserve(仅 CSP) | 缺失 | 有(1.0) |
| buy-hold | 有(引擎一等) | 无(刻意) |

后果:Web 表单无法配置引擎支持的参数;watchlist.Validate 拒绝引擎 Factory 接受的参数。

## 实施(Step A,对齐不动架构)

- `watchlist.go`:`expiry_rule` Choices 加 `days`;cc 补 `lot_size`(100.0);CSP 补 `lot_size` + `cash_reserve`(1.0);注释更新指向排期登记
- `template_test.go`:对齐断言(choices 双值、lot_size/cash_reserve 默认、cash_reserve 仅 CSP 合法用例)
- `httpapi/watchlist_test.go`:契约断言更新(choices ≥2、csp cash_reserve)
- `doc/API.md:74`:「模板落地前,本端点按草稿契约硬编码」→ 现状 + 排期指向
- 新增排期登记 `doc/tasks/2026-08-03-watchlist-template-registry.md`(Step B 统一注册表)

## 验证

- go test 受影响包全绿;verify.sh 连跑两遍全绿
- 端到端:serve + OrbStack PG,`/v1/strategies` 返回 cc 5 参数/CSP 6 参数、expiry_rule=[next_expiry, days]、cash_reserve=1.0
- CI 五检查全绿

## 遗留

- **Step B**(排期登记中):/v1/strategies 改从 `internal/strategy` 注册表渲染 + Validate 迁移 + 删硬编码——唯一来源化
- 前端表单零改动(choices/number 通用渲染,自动生效)
