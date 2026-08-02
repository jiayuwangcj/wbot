# 闭环 #53+#54: 远程 PG 验收 + watchlist buy-hold 模板修复

- **日期**: 2026-08-03
- **PR**: #226(功能)+ 本文档(归档)
- **背景**: #52 把零依赖 accept 脚本(papaker/agent-federation)升为 test job 门禁后,PG 依赖对(backtest/watchlist)仍是本地专属。db-integration job 已有 postgres service 且种子可用 `wbot ingest mock`(本地 mock 数据),远程化条件成熟。

## 改动(#53: 远程验收)

- ci.yml db-integration 新步骤「Remote PG-backed acceptance」: mock 种子(与 dev-up 同款)+ 起 serve + accept-backtest.sh + accept-watchlist.sh
- 验收远程化分层完整: 零依赖对(test job)→ PG 依赖对(db-integration)→ 网关依赖(futu 系)永不进 CI

## 修复(#54: CI 严格模式暴露的真实缺陷)

新步骤 set -euo pipefail 立即暴露 dev-up 种子静默失败:

- **缺陷**: watchlist PUT buy-hold → 400(模板注册表只有 covered-call/cash-secured-put),被 dev-up 种子 `curl ... || true` 吞掉——watchlist 表实为空,「回测 watchlist 模式」(dev-up 声称可跑)从未真正可用(实测 POST from_watchlist → 422 empty_watchlist)
- **修复**: 模板注册表加 buy-hold(引擎一等策略,无 params);GET /v1/strategies 契约 2→3 模板(测试同步);dev-up 种子去 `|| true`;accept-watchlist +buy-hold 检查(16 项);accept-backtest +from_watchlist 201 端到端(21 项,回测 watchlist 模式首次端到端验证: runs=2 全 buy-hold)
- **顺带修复**: accept-backtest CLI -export 显式传 -dsn(此前依赖 shell 环境变量 WBOT_PG_DSN,无 env 时 exit 2 + 等价检查空串——#47 时环境恰好有 env 掩盖了)

## 验证

- dev-up 16/16(严格化种子真上);from_watchlist 实测 201 runs=2;accept-backtest 21/21 + accept-watchlist 16/16 连跑两遍稳定;CI 5/5(db-integration 含远程验收)
- 历史残留数据(option-watch 旧模板名)暴露批处理「首败中止」语义: from_watchlist 一条脏数据拖垮整批——清理后恢复

## 备注

- **引擎经验**: ①验收脚本升远程门禁是有价值的工程动作——严格 shell 模式(set -euo pipefail)能暴露被 `|| true` 掩盖多年的真实缺陷;②验收脚本不得依赖 shell 环境变量传递(脚本有参数就该全程显式传);③「声称可跑」的功能(回测 watchlist 模式)必须有端到端断言,不能只信种子提示输出。
- **候选池**: 仍枯竭。
