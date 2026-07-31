# 产品组目标：PC Web（v1）+ 后台管理 + 券商 paper 接入

- **id**: `2026-07-31-web-v1-target`
- **created**: `2026-07-31`
- **updated**: `2026-07-31`

## Goal

老板（用户）目标（2026-07-31 产品组，重大变更）：

1. **放弃微信小程序**（微信会下架包含股票的小程序）——停止小程序开发，废弃既有小程序相关代码/拆解
2. **转向 Web 端**：第一步实现 **PC 端 Web**，预留**移动版本 Web 框架**（响应式）
3. **后台管理页面（all-in-one）**保持不变（本就是 Web 形态）：后端运行状态/简要设置/配置/集群状态；微信/Schwab/IBKR 凭据配置入口（配置值仍存 `~/.wbot/`，PRIVACY 红线）
4. **券商 paper trading 接入**（2026-07-31 追加）：**Schwab 与 IBKR 均提供 paper 功能**（嘉信 PaperMoney、IBKR Paper Trading 模拟账户）——先接入；如需要老板协助注册账号/获取凭证，汇总到 discussions/21 告知

遇到不可抗阻碍（需老板操作的部分）**暂时挂起，继续开发后面的部分**。

## 拆解

| 切片 | 状态 | 依赖 |
| --- | --- | --- |
| ① 后端数据 API（/v1/bars、/v1/runs、/v1/health、/v1/admin/*） | ✅ 已完成（a26436c、#24、#33、#36、#37） | 无 |
| ② API 契约文档（doc/API.md） | ✅ 已完成 | ① |
| ⑧ PC 端 Web（前端，第一步）：页面框架 + 数据展示（bars/runs）+ 后台管理页面 | 可做（go:embed Web UI，ROADMAP v4 路线；不依赖微信工具链） | ① |
| ⑨ 移动版本 Web 框架预留（响应式布局/断点，PC 页面可复用） | 可做（随 ⑧ 布局层预留，不单独阻塞） | ⑧ |
| ⑩ 券商 paper 接入：Schwab PaperMoney / IBKR Paper | **部分挂起**——需老板注册 paper 账号/API 凭证（discussions/21 告知） | ① |
| ④ 微信小程序前端 | ❌ **废弃**（微信将下架含股票小程序，2026-07-31 老板指令） | — |
| ⑤ 券商持仓显示 | **挂起**——缺 paper/真实凭证（并入 ⑩） | ⑩ |

> 注：原 miniapp 目标见 [[2026-07-31-miniapp-v1-target]]（已废弃，指向本文件）。

## Links

- 老板反馈主题：[discussions/21「需要老板处理的」](https://github.com/jiayuwangcj/wbot/discussions/21)（微信相关项解除；券商 paper 账号需求加入）
- 需求源：[discussions/10](https://github.com/jiayuwangcj/wbot/discussions/10)
- [[ROADMAP]] v4（go:embed Web UI 路线）、[[PRIVACY]]（凭证放 `~/.wbot/`，永不入库）

## State

- **status**: `running`
- **last step**: 2026-07-31 老板指令：放弃小程序转 Web（PC 优先 + 移动预留）；券商 paper 先接入；后台管理 ⑥⑦ 保留（已并入 ① 完成/⑧ 前端）。

## Next

- 切片 ⑧（PC Web 前端）：由 PM 组排单（go:embed 静态页 + bars/runs/admin 页面，可测）；⑨ 随 ⑧ 布局预留；⑩ 券商 paper 账号需求更新 discussions/21 待老板。
