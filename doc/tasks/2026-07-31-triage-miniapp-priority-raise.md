# 分诊结案：提高微信小程序实现优先级

- **id**: `2026-07-31-triage-miniapp-priority-raise`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

响应 [discussions/9](https://github.com/jiayuwangcj/wbot/discussions/9) 人工留言「提高微信小程序实现的优先级」：拆解依赖、调整 ROADMAP、启动前置的 Go API。

## Constraints

- 券商持仓数据硬依赖凭证：不虚构、不伪接入，标记 blocked。
- 小程序前端不抢跑：先做 Go API 数据接口。

## Links

- 触发: <https://github.com/jiayuwangcj/wbot/discussions/9#discussioncomment-17851977>（人工留言，2026-07-31 09:09）
- [[ROADMAP]]、[discussions/10](https://github.com/jiayuwangcj/wbot/discussions/10)

## State

- **status**: `done`
- **last step**: `[robot]` 分诊回复（comment `17851991`）：① Go API 提前实施（现数据即可）② 券商持仓接入 blocked（缺凭证）③ 小程序前端待 ① 完成。`doc/ROADMAP.md` v3/v4 行更新（Go API 提前、小程序优先级提高、持仓数据接口标注 blocked）。

## Next

- 实施第 ① 刀：Go API 数据接口（`internal/httpapi` + `wbot serve`：GET /v1/bars、/v1/runs），见后续任务记录。
