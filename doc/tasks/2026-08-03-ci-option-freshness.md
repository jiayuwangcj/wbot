# 闭环 #56: option-freshness 验收远程化(db-integration)

- **日期**: 2026-08-03
- **PR**: #230(功能)+ 本文档(归档)
- **背景**: #52/#53 落地后,验收远程化分层仍有缺口: option-freshness 属 PG 依赖对(option_quotes 表、无网关依赖),按引擎自身原则应进 db-integration,却因依赖 docker(wbot-pg-ci-test 地址发现 + docker exec psql 种子)滞留本地专属。

## 改动

- **脚本** `scripts/accept-option-freshness.sh`: 签名改 `[bin] [dsn]` 可选参数;DSN 解析链 参数 → `$WBOT_PG_DSN` → docker 桥接发现;SQL 执行器 psql 优先、docker exec 兜底(OrbStack 开发机无 psql 客户端——本地路径保持 docker,CI 路径用 psql);freshness 调用从 `WBOT_PG_DSN=` 环境变量改为显式 `-dsn`(#53 纪律: 验收脚本不得依赖环境变量传递)
- **CI** `.github/workflows/ci.yml` db-integration: 新步骤安装 postgresql-client;验收步骤末尾跑 `scripts/accept-option-freshness.sh "$bin" "$WBOT_PG_DSN"`
- **文档** `doc/ACCEPTANCE.md`: 脚本表 option-freshness 行加 ✅ db-integration + 运行方式更新;顶层表「何时跑」引用补 #56

## 验证

- 本地: 无参路径(docker 兜底)与显式参数路径各连跑两遍 6/6 稳定;verify.sh 全绿
- CI 5/5: db-integration 1m35s 含新步骤——mock bars(2024-06-01 固定)恒 stale,默认阈值 run 必 exit 1、-max-age 覆盖必 exit 0,6 项检查确定性成立(先读 mock.go 确认种子时间戳再接入,避免 CI 首跑翻车)

## 备注

- **引擎经验**: ①验收远程化分层的收官判定——脚本依赖按「零依赖 / PG 依赖 / 网关依赖」归类,PG 依赖对不该因「本地用 docker 跑着方便」而滞留本地;②远程化前先读数据源实现确认 CI 初始状态确定性(mock 种子时间戳固定 → freshness 门禁行为可预测),而不是靠「可能碰巧」;③docker exec 类本地路径需保留为兜底,CI 走标准客户端,两条路径同一个脚本验证。
- **验收远程化分层至此完整**: 零依赖对(test job: paper/agent-federation)→ PG 依赖对(db-integration: backtest/watchlist/option-freshness)→ 网关依赖(futu 系 + bars-refill + options-ingest 等)永不进 CI。
- **候选池**: 仍枯竭(待老板 7 项 + 微信小程序 blocked)。
