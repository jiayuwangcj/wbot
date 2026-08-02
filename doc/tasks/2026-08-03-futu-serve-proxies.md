# 闭环 #63: FUTU.md serve 代理表补全

- **日期**: 2026-08-03
- **PR**: #244(功能)+ 本文档(归档)
- **背景**: AUTO_ADVANCE triage 对账维度「FUTU.md vs serve 实际注册」——serve mux 注册 4 个 futu 代理(main.go:1584-1587:quote/account/orders/options),API.md 引言「见 [[FUTU]]」指向本文件,但 FUTU.md §7 serve 代理段只描述了 quote 一个。

## 改动

- §7 serve 代理段扩为 4 行表(端点/行为/网关地址):account/orders 走 proto `FUTU_PROTO_ADDR`(11111,与 CLI 同客户端同安全策略)、quote/options 走 REST `FUTU_GATEWAY_URL`(22222);契约指向 API.md 各节

## 验证

- 与 main.go:239 serve help 文本、API.md:448/481/551/589 四节逐条对齐;docs-only → CI skip 路径 5/5

## 备注

- **引擎经验**: 新对账维度——「权威引用文件的覆盖度」:API.md 写「见 [[FUTU]]」把 futu 权威指向 FUTU.md,但 FUTU.md 未覆盖全部被引用端点;对账要查「被 [[..]] 引用的文件是否实际覆盖了引用它的内容」。
- **候选池**: 仍枯竭(待老板 7 项)。
