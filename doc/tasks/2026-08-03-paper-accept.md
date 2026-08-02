# 闭环 #42: `wbot paper` CLI 契约验收脚本

- **日期**: 2026-08-03
- **PR**: #204(脚本 + 归档合一)
- **背景**: 「验收覆盖扩展」引擎收官: 逐个核对所有子命令的验收覆盖后,paper 是最后一个零验收子系统——verify.sh/CI 仅有 `paper -symbol V.US -side buy` 的启动冒烟(「启动冒烟 ≠ 验收」)。CLI 契约(默认值/side 别名/退出码)此前只有单元测试。

## 改动

`scripts/accept-paper.sh`(纯本地,无网络无 DB):

- 默认值: DEMO.US buy,exit 0,输出 `side=/status=/id=` 形状(`paper-1` 序 id)
- `-symbol`/`-side` 覆盖;side 别名 b/B/s/S
- 错误契约: 非法 side → exit 2 + stderr 提示「unknown side "bogus"」;空 symbol → 引擎校验 exit 1「invalid symbol」

## 验证

- 12/12 连跑两次稳定
- CI 5/5 全绿

## 备注

- **引擎收官**: 至此全部子命令(master/agent/paper/ingest 系/serve/backtest/futu 系/watchlist)均有 accept 脚本或既有 e2e 验收(dev-up 10 项含 futu 系);「验收覆盖扩展」引擎候选清零。
- **契约细节**: `status=3` 为 paper.Engine 的模拟成交状态;`id=paper-N` 每次进程独立从 1 计(无持久化)。
- **候选池**: 仍枯竭。
