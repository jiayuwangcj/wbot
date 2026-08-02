# 闭环 #48: futu 数据面 HTTP 端点端到端验收

- **日期**: 2026-08-03
- **PR**: #216(功能)+ 本文档(归档)
- **背景**: #47 的验收分层经验(子命令级 vs 数据面级)收官——futu 系 HTTP 端点(GET /v1/futu/quote|orders|account)此前只有 doc/API.md 契约 + 单测,零 e2e 脚本。dev-up 刻意不含网关依赖(注释写明由 accept 覆盖),但 futu 系 accept 脚本此前只覆盖 CLI(funds/options ingest)与 snapshots 端点——三个数据面端点本身是验收盲区。

## 改动

新增 `scripts/accept-futu-data.sh`(15 项检查,连真实网关):

- **quote**: 200 + `basic_qot_list` 形状(cur_price 数值/name 字符串);缺 symbol → 400;非法 symbol → 400
- **orders**: 200 + env/acc_id/orders 数组 + 白名单键(order_id/symbol/status/side);`env=real` 参数通道生效;env/acc_id/pending 非法 → 400
- **account**: 200 + funds 白名单键(total_assets/cash/market_val/available_cash/power)全数值 + total_assets>0 + positions 数组;`env=real` 通道(实测 real 2830.26);错误契约 → 400

## 验证

- 真实数据实测: quote HK.00700 cur_price 475.2;orders 1 笔 OrderStatus_Submitted;account sim 1907141 / real 281756478875559548
- 连跑两遍 15/15 稳定 + CI 5/5

## 备注

- **引擎经验**: 验收覆盖的盲区往往藏在「注释写明分工」的地方——dev-up 说 futu 由 accept 覆盖,但 accept 脚本清单也要按**端点级**对账,不能只按子系统/子命令级对账(#47 经验的应用)。至此 serve 全部 HTTP 面(DB 本地 16 项 dev-up + futu 数据面 15 项 + snapshots 6 项 + cluster/options 系)均有 e2e 验收。
- **候选池**: 仍枯竭。
