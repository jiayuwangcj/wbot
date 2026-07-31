# 文档：API 契约 + 小程序前端 blocked

- **id**: `2026-07-31-api-contract-doc`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

微信小程序路径第 ② 步（① Go API 已完成）：`doc/API.md` 契约文档（端点/参数/响应示例/错误/本地验证），供小程序与 Web 前端对齐；小程序前端骨架标记 blocked（缺微信开发者工具与账号）。

## Constraints

- 纯文档；不改代码。

## Links

- [[ROADMAP]] v4、[discussions/9](https://github.com/jiayuwangcj/wbot/discussions/9) 分诊（提高小程序优先级）
- 前置：`doc/tasks/2026-07-31-httpapi-serve.md`（已完成）

## State

- **status**: `done`
- **last step**: 新建 `doc/API.md`（/v1/runs、/v1/bars 契约 + 错误表 + 本地验证命令）；`doc/README.md` 索引加 [[API]]。

## Next

- 小程序前端骨架：**blocked**——缺微信开发者工具（miniprogram 工具链）与小程序账号；就绪后拆最小步（先用 /v1/bars、/v1/runs 展示已有数据）。
- 券商持仓数据（v3）：**blocked**——缺 Schwab/IBKR 凭证。
