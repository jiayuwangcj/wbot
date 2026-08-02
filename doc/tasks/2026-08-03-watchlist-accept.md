# 闭环 #49: wbot watchlist CLI 子命令端到端验收

- **日期**: 2026-08-03
- **PR**: #218(功能)+ 本文档(归档)
- **背景**: #47 验收分层经验(子命令级 vs 数据面级)继续应用——`wbot watchlist` CLI(add/remove/list)是最后一个零验收覆盖的子命令: verify.sh 无 watchlist 冒烟,dev-up 只覆盖 HTTP 面(PUT/GET /v1/watchlist)。CLI 契约(错误码/输出形状/幂等 Upsert)此前只有单测。

## 改动

新增 `scripts/accept-watchlist.sh`(14 项检查,连真实 PG,独立 symbol ACCEPT.US 不留痕):

- **错误契约**: 缺 -symbol / 缺 -strategy → exit 2;非法 -params JSON / 非法 strategy → exit 2;remove 不存在 → exit 1
- **add**: 成功 + 输出形状(`watchlist: SYM strategy=… params=…`);幂等 Upsert(重复 add 同 symbol 改 params 生效)
- **list**: `SYM STRAT params` 行形状
- **写面→读面联动**: CLI add 后 HTTP GET /v1/watchlist 可见;remove 后不可见(清理验证)

## 验证

- dev-up 环境连跑两遍 14/14 稳定
- 实测修正脚本自身断言方向错误:「不含」检查须 want grep 计数 0(首版写反)
- CI 5/5

## 备注

- **引擎经验**: 子命令级验收清单继续按「verify.sh 有无冒烟 + dev-up 有无覆盖 + accept 有无脚本」三维对账——watchlist 是 verify.sh 无冒烟、dev-up 仅 HTTP、accept 无脚本的最后一个;至此全部 CLI 子命令(serve/ingest/futu/backtest/paper/watchlist/agent/configyaml/master)均有 e2e 或冒烟验收。futu order(真实下单面)刻意不设自动脚本(写操作 + 账户状态变更,敏感面)。
- **候选池**: 仍枯竭。
