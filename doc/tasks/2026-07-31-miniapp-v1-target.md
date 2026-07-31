# 产品组目标：小程序 + all-in-one 后台管理（尽快可用）

- **id**: `2026-07-31-miniapp-v1-target`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

老板（用户）目标（2026-07-31 产品组，07-31 更新）：**尽快有一个可用的版本**，包含两部分：

1. **微信小程序**（前端）——股票数据/回测结果查看；
2. **后台管理页面（all-in-one）**——显示后端服务器的**运行状态、简要设置、配置、集群状态**等，且**微信、Schwab、IBKR 等凭据配置在这里配置**（配置值仍存 `~/.wbot/`，见 PRIVACY 红线，页面仅提供入口）。

遇到不可抗阻碍（需老板操作的部分）**暂时挂起，继续开发后面的部分**。

## 拆解

| 切片 | 状态 | 依赖 |
| --- | --- | --- |
| ① 后端数据 API（/v1/bars、/v1/runs） | ✅ 已完成（a26436c） | 无 |
| ② API 契约文档（doc/API.md） | ✅ 已完成（b9fbe67） | ① |
| ③ 小程序所需后端增强（GET /v1/health 含 DB ping） | ✅ 已完成（PR #24） | ① |
| ⑥ 后台管理后端（all-in-one 数据面）：运行状态/设置/配置/集群状态 API | 可做（可测，不依赖微信工具链） | ①③ |
| ⑦ 后台管理配置入口：微信/Schwab/IBKR 凭据配置 | 部分挂起——微信 token 等老板提供（discussions/21） | ⑥ |
| ④ 小程序前端骨架/页面 | **挂起**——缺微信开发者工具与小程序账号（[discussions/21](https://github.com/jiayuwangcj/wbot/discussions/21) 待老板处理） | ①+③、账号/工具 |
| ⑤ 券商持仓显示 | **挂起**——缺 Schwab/IBKR 凭证（discussions/21） | ③+④、凭证 |

> 注：切片编号保留原 ①-⑤（历史），后台管理新增 ⑥⑦。

## Links

- 老板反馈主题：[discussions/21「需要老板处理的」](https://github.com/jiayuwangcj/wbot/discussions/21)
- 需求源：[discussions/10](https://github.com/jiayuwangcj/wbot/discussions/10)
- [[ROADMAP]] v4（Go API + go:embed Web UI，后台管理前端可复用此路线；微信小程序优先级已提高）
- [[PRIVACY]]（微信 token 放 `~/.wbot/`，配置值永不入库）

## State

- **status**: `running`
- **last step**: 切片③ 完成合入（PR #24/#27）；目标扩展后台管理页面（2026-07-31 老板指令）。

## Next

- 切片 ⑥（可做）：由 PM 组排单（后台管理后端 API 设计：运行状态/设置/配置/集群状态，可测）；⑦ 待老板资源就绪后解除挂起。
