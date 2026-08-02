# 闭环 #57: options-cluster 验收远程化(db-integration)

- **日期**: 2026-08-03
- **PR**: #232(功能)+ 本文档(归档)
- **背景**: #56 归档声称「验收远程化分层完整」,但 triage 对账 ACCEPTANCE.md 脚本表发现 accept-options-cluster.sh(2 项,serve + PG 前置)仍未标 CI——它与 option-freshness 同构(纯 SQL 种子 + HTTP 断言,**无网关依赖**),属 PG 依赖对,按分层原则应进 db-integration。「分层完整」声明自身就是 #55 式盲区: 每个新声明都要回到脚本表逐行对账。

## 改动

- **脚本** `scripts/accept-options-cluster.sh`: 签名改 `[base-url] [dsn]`;DSN 解析链 参数 → `$WBOT_PG_DSN` → docker 桥接发现;SQL 执行器 psql 优先、docker exec 兜底(#56 同款);自种子 ACCCLUS* 行(fresh/stale)断言 /v1/admin/cluster 的 options_freshness 字段与 bars_coverage 向后兼容
- **CI** `.github/workflows/ci.yml` db-integration: 验收步骤末尾加 `scripts/accept-options-cluster.sh http://127.0.0.1:8080 "$WBOT_PG_DSN"`(serve 已在跑;psql 客户端复用 #56 安装步骤,步骤名改「acceptance seeding」)
- **文档** `doc/ACCEPTANCE.md`: 该行加 ✅ db-integration + 运行方式更新;顶层表引用补 #57

## 验证

- 本地: 无参路径(docker 兜底)与显式参数路径连跑 2/2 稳定;verify.sh 全绿
- CI 5/5: db-integration 1m31s(四脚本全跑,无新增依赖)

## 备注

- **引擎经验**: ①「分层完整」这类绝对声明落地后,下一轮 triage 必须回到 ACCEPTANCE.md 脚本表逐行对账 CI 列——声明本身不是证明,脚本表才是;②同构脚本的远程化是复制粘贴级改造(#56 模式直接套用),验证重点在两条路径(本地 docker 兜底 / CI psql)各自能跑通。
- **验收远程化分层现状**: 零依赖对(test job: paper/agent-federation)→ PG 依赖对(db-integration: backtest/watchlist/option-freshness/options-cluster)→ 网关依赖(futu 系 + bars-refill + options-ingest 等)永不进 CI。
- **候选池**: 仍枯竭(待老板 7 项 + 微信小程序 blocked)。
