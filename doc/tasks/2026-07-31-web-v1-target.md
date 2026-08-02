# 产品组目标：PC Web（v1）+ 后台管理 + 券商 paper 接入

- **id**: `2026-07-31-web-v1-target`
- **created**: `2026-07-31`
- **updated**: `2026-08-02`

## Goal

**今日版本目标（2026-08-01 老板指令）**：可用的**模拟盘回测** + **模拟盘策略运行**（covered call / cash-secured put 等）+ **可调参**（watchlist/每标的参数）+ **Web 后端可用**（serve API + 管理页）。—— **✅ 已达成（2026-08-01）：** 数据层（#67：期权/正股/回测结果落库+缓存）、交易命令（#66：funds/position/order，模拟盘优先护栏）、策略实现（#69：covered-call/cash-secured-put+期权腿结算）、watchlist 调参（#68：/v1/watchlist+CLI）、Web 后端全链可用。

老板（用户）目标（2026-07-31 产品组，重大变更）：

1. **放弃微信小程序**（微信会下架包含股票的小程序）——停止小程序开发，废弃既有小程序相关代码/拆解
2. **转向 Web 端**：第一步实现 **PC 端 Web**，预留**移动版本 Web 框架**（响应式）
3. **后台管理页面（all-in-one）**保持不变（本就是 Web 形态）：后端运行状态/简要设置/配置/集群状态；微信/Schwab/IBKR 凭据配置入口（配置值仍存 `~/.wbot/`，PRIVACY 红线）
4. **券商接入：富途优先**（2026-07-31 追加，老板指令）——**先接入富途 API**：
   - **Futu OpenD 用容器方式运行部署**，docker-compose 记住配置（configs/）
   - Go 语言使用 **proto 接口连接 OpenD 客户端**；可参考/直接使用 **qtopie/gofutuapi**（Go 客户端库）
   - Schwab/IBKR（含 paper）**降级为后续候选**，凭证需求仍列 discussions/21

遇到不可抗阻碍（需老板操作的部分）**暂时挂起，继续开发后面的部分**。

## 拆解

| 切片 | 状态 | 依赖 |
| --- | --- | --- |
| ① 后端数据 API（/v1/bars、/v1/runs、/v1/health、/v1/admin/*） | ✅ 已完成（a26436c、#24、#33、#36、#37） | 无 |
| ② API 契约文档（doc/API.md） | ✅ 已完成 | ① |
| ⑧ PC 端 Web（前端，第一步）：页面框架 + 数据展示（bars/runs）+ 后台管理页面 | ✅ 已完成（PR #45/#49/#50，/ui/ 上线） | ① |
| ⑨ 移动版本 Web 框架预留（响应式布局/断点，PC 页面可复用） | ✅ 已完成（viewport/断点/auto-fit 已预埋） | ⑧ |
| ⑩ 券商 paper 接入：Schwab PaperMoney / IBKR Paper | **挂起**（降级候选；富途优先） | ① |
| ⑪ 富途接入：OpenD 容器部署（docker-compose 配置）+ Go proto 客户端（参考/直接使用 qtopie/gofutuapi） | **可做**（OpenD 容器本地部署；行情数据接入；需富途账号登录 OpenD——老板协助时列 discussions/21） | ① |
| ④ 微信小程序前端 | ❌ **废弃**（微信将下架含股票小程序，2026-07-31 老板指令） | — |
| ⑤ 券商持仓显示 | **挂起**——缺 paper/真实凭证（并入 ⑩） | ⑩ |


| ⑫ 策略模块：Covered Call + Cash-Secured Put + 关注标的（watchlist） | **可做**（拆解中：期权数据依赖/回测集成/参数待产品组细化；**测试标的：HK.00700（腾讯，模拟盘）**——2026-08-01 老板确认） | ①（数据）、富途期权行情（⑪ 链路） |

> 注：原 miniapp 目标见 [[2026-07-31-miniapp-v1-target]]（已废弃，指向本文件）。

## Links

- 老板反馈主题：[discussions/21「需要老板处理的」](https://github.com/jiayuwangcj/wbot/discussions/21)（微信相关项解除；券商 paper 账号需求加入）
- 需求源：[discussions/10](https://github.com/jiayuwangcj/wbot/discussions/10)
- [[ROADMAP]] v4（go:embed Web UI 路线）、[[PRIVACY]]（凭证放 `~/.wbot/`，永不入库）

## State

- **status**: `running`
- **last step**: 2026-08-02 ⑪-b/c 完成（富途 Go 客户端+数据管道 PR #58-#61）；⑫ 策略模块（covered-call/cash-secured-put #69）、watchlist 调参（#68）、Web 全链可用（#45-#90）；⑧⑨ 完成；8-02 凌晨 20 个 PR（#71-#90：策略工作台/多 symbol 回测/导出/新鲜度监控/富途账户与期权 Web）全部合入，vdaily-20260802 已从 main 重建并部署（~/.wbot/releases/daily-20260802/，校验 OK）。

## Next

- ⑩ Schwab/IBKR paper：降级挂起（凭证 blocked，discussions/21）；富途实盘下单 Web 化需老板确认（doc/FUTU.md 交易安全策略）。
- 产品组下一批草稿（doc/issues/2026-08-02-* 已全部交付）：批量导出/图表报告、新鲜度告警推送（等老板通知 token）、自动补拉。
