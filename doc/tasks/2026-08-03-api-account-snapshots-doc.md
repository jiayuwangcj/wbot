# 闭环 #33: API.md 补 GET /v1/account/snapshots 端点文档

- **日期**: 2026-08-03
- **PR**: #186(功能+归档合一, doc-only)
- **背景**: S-account-curve UI 闭环(#30, PR #181)只更新了 serve help 文案,**遗漏 API.md**——仓库惯例是 API.md 必须收录每个端点(#25 曾因 POST /v1/ingest 缺文档补过)。triage 全量扫描 API.md 端点清单时发现 `/v1/account/snapshots` 全篇无提及(总览与章节均缺),是 S-account-curve 三段(数据层 #179 / UI #181 / 文档 #183)后的**文档欠账**。

## 改动

`doc/API.md`:

- 第 3 行数据面接口总览补 `/v1/account/snapshots`(资产曲线历史快照, 读 `account_snapshots` 表)
- `/v1/futu/account` 章节后新增 `## GET /v1/account/snapshots` 完整章节:
  - 定位: 资产曲线数据面, 与 `/v1/futu/account`(实时网关代理)互补——一查(CLI)一存(表)一读(本端点);纯 DB 数据面, 无网关依赖、无凭证
  - Query 参数: `env`(缺省 sim;sim/simulate/paper → simulate、real → real、非法 → 400,归一化语义同 futu/account)、`limit`(缺省 120, 1..10000, 非法 → 400)
  - 响应 `200` 示例: `{env, limit, points:[{captured_at RFC3339 UTC, total_assets, cash, market_val}]}` 时间递增;无数据空数组
  - 错误: `400`(invalid_request)、`500`(内部)、非 GET → `405`;无 `503`(不走网关)
  - 交叉引用 [[DATA_PIPELINE]] 资产快照章与 [[FUTU]] §9(写入侧 `wbot ingest account`)

## 验证

- 文档内容逐项与 `internal/httpapi/account_snapshots.go` 实现核对(默认值/归一化/边界/错误码/字段名/格式)
- grep 断言: API.md 含端点两处(总览 + 章节)
- CI 5/5 全绿;无 Go 改动

## 备注

- **经验**: 新端点落库后, API.md 收录应与 serve help 文案同步做——本闭环是 #30 的欠账补还;检查端点文档覆盖时以 API.md 章节列表 grep 为准(本次就是靠 `grep -n "v1/"` 全表对账发现)。
- 候选池依旧枯竭(同 #32 备注);本步取自动「文档欠账对账」——每轮 triage 顺带对账 API.md 端点清单与 serve mux 注册。
