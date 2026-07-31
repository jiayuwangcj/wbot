# 产品组目标：尽快可用的小程序版本（阻塞挂起、可做继续）

- **id**: `2026-07-31-miniapp-v1-target`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

老板（用户）目标（2026-07-31 产品组）：**尽快有一个可用的小程序版本**；遇到不可抗阻碍（需老板操作的部分）**暂时挂起，继续开发后面的部分**。

## 拆解

| 切片 | 状态 | 依赖 |
| --- | --- | --- |
| ① 后端数据 API（/v1/bars、/v1/runs） | ✅ 已完成（a26436c） | 无 |
| ② API 契约文档（doc/API.md） | ✅ 已完成（b9fbe67） | ① |
| ③ 小程序所需后端增强（如健康检查） | 可做（可测，不依赖微信工具链） | ① |
| ④ 小程序前端骨架/页面 | **挂起**——缺微信开发者工具与小程序账号（[discussions/21](https://github.com/jiayuwangcj/wbot/discussions/21) 待老板处理） | ①+③、账号/工具 |
| ⑤ 券商持仓显示 | **挂起**——缺 Schwab/IBKR 凭证（discussions/21） | ③+④、凭证 |

## Links

- 老板反馈主题：[discussions/21「需要老板处理的」](https://github.com/jiayuwangcj/wbot/discussions/21)
- 需求源：[discussions/10](https://github.com/jiayuwangcj/wbot/discussions/10)
- [[ROADMAP]] v4（微信小程序优先级已提高）、[[PRIVACY]]（微信 token 放 `~/.wbot/`）

## State

- **status**: `queued`
- **last step**: 目标与拆解落库；discussions/21 汇总待老板处理项。

## Next

- 切片 ③（可做）：由 PM 组排单（如 `wbot serve` 增加 GET /v1/health，含 DB ping；可测）；④⑤ 待老板资源就绪后解除挂起。
