# 产品组目标：微信小程序（已废弃，转 Web）

- **id**: `2026-07-31-miniapp-v1-target`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`
- **status**: `superseded`

## 废弃说明（2026-07-31 老板指令）

微信会下架包含股票的小程序——**放弃微信小程序的开发和所有代码**，目标转向 **PC 端 Web（第一步）+ 移动 Web 框架预留**。新目标见 [[2026-07-31-web-v1-target]]。

- 切片④（小程序前端）取消；切片⑤（券商持仓）并入 Web 目标切片⑩（paper 接入）
- 切片①-③（后端数据 API/契约/健康检查）与 ⑥⑦（后台管理后端）**不受影响**，已并入 Web 目标完成项
- discussions/21 中「微信开发者工具/小程序账号」待老板项**解除**

## 历史拆解（保留备查）

| 切片 | 状态 | 依赖 |
| --- | --- | --- |
| ① 后端数据 API（/v1/bars、/v1/runs） | ✅ 已完成（a26436c） | 无 |
| ② API 契约文档（doc/API.md） | ✅ 已完成（b9fbe67） | ① |
| ③ 小程序所需后端增强（GET /v1/health 含 DB ping） | ✅ 已完成（PR #24） | ① |
| ⑥ 后台管理后端（all-in-one 数据面） | ✅ 已完成（PR #33/#36/#37） | ①③ |
| ⑦ 后台管理配置入口：微信/Schwab/IBKR 凭据配置 | 部分挂起（并入 Web 目标） | ⑥ |
| ④ 小程序前端骨架/页面 | ❌ 废弃 | — |
| ⑤ 券商持仓显示 | 挂起（并入 Web 目标切片⑩） | — |

## Links

- 老板反馈主题：[discussions/21「需要老板处理的」](https://github.com/jiayuwangcj/wbot/discussions/21)
- 需求源：[discussions/10](https://github.com/jiayuwangcj/wbot/discussions/10)
- 新目标：[[2026-07-31-web-v1-target]]、[[ROADMAP]]、[[PRIVACY]]
