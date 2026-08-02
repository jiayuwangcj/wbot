# 闭环 #32: FUTU.md 补 `ingest account`(资金快照持久化孪生命令)文档

- **日期**: 2026-08-03
- **PR**: #184(功能+归档合一, doc-only)
- **背景**: S-account-curve 三段(#179 数据层 / #181 UI / #183 文档)全闭合后,DATA_PIPELINE.md 已含 `wbot ingest account` 的调度说明,但 futu 专属参考文档 FUTU.md §9(`wbot futu funds/position/order` 交易命令)未提及这个同数据线的**落库孪生命令**——读 FUTU.md 的读者无从知道 funds 查询有持久化路径。巡检发现缺口,补文档。

## 改动

`doc/FUTU.md` §9 末尾新增小节「资金快照持久化 `wbot ingest account`(2026-08-03 实测)」:

- 定位: 与 `wbot futu funds` 同一 OpenD protobuf funds 查询(TCP 11111, 只读, 安全面相同)的落库孪生命令,写 `account_snapshots`(migration 004, 幂等)
- 命令示例: `wbot ingest account [-env sim|real] [-acc-id X] [-addr 127.0.0.1:11111] [-dsn] [-every 1h]`(flags 定义与 usage 文案经 cmd/wbot/ingest_account.go 核实)
- 语义: `-env`/`-acc-id`/`-addr` 与 §9 交易命令一致;`-env real` 同样只读;`-every` 与外部 cron 二选一 → 交叉引用 [[DATA_PIPELINE]] 资产快照章
- 实测: sim 盘 acc 1907141 total_assets=1198286.82 —— 与 §9 实测记录表 `wbot futu funds` 一致(同一 funds 数据线, 一查一存)
- 隔离: 账户快照与 `ingestion_runs` 隔离([[PRIVACY]])

## 验证

- 内容断言 grep:FUTU.md 含新小节标题/命令示例/实测数值/交叉引用
- CI 5/5 全绿(test / db-integration / governance / check-skip / ci-summary)
- 无 Go 改动 → gofmt/本地测试不受影响

## 备注

- **本轮 triage 新认知**: 「响应式/移动端布局」候选**已存在**——style.css:694 `@media (max-width: 767px)` 块已覆盖窄屏(44px 触控目标、表单竖排、nav 块状、table min-width 600px + .table-scroll 横滚),此前评估以为无窄屏断点是误判。移动端候选从候选池移除,勿重复做。
- 候选池现状: doc/issues/ 13 个 draft 全 ✅;ROADMAP 剩余 blocked(多 symbol 时间对齐, 待拍板);待老板 7 项无一项可自主推进;微信小程序需凭证 blocked。
- 下一步候选: 无自主可推进项,工程进入等待状态;等老板拍板/资源/新需求。
