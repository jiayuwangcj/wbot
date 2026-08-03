# ROADMAP

高层里程碑（细节用 issue 跟踪）。

## 状态

- [[PLAN_V0]] 已完成：自动化与治理基线
- v1 已完成：数据管道（拉取/落地 PostgreSQL/校验/查询/导出/调度韧性），见 `doc/tasks/2026-04-18-*`、`2026-07-31-ingest-*`（产品组 2026-07-31 整理归档，issue #1/#2 已关）
- v2 主体已完成：回测运行器、约束、费用占位、DSN 输入；多 symbol 时间对齐设计 `blocked`（待拍板，见 `doc/tasks/2026-07-31-backtest-multisymbol-design.md`）

## 工作优先级（路线）

**回测依赖可追溯的历史数据**：没有**拉取、校验、可重复落地**的数据集，回测只能是空转。因此任务排期遵循：

1. **数据拉取与落地** — 最高优先（数据源抽象、拉取任务、持久化、完整性/元数据）。
2. **回测** — 在已落地数据上建立可测的回测运行与最少指标。
3. **执行与模拟盘** — 在 1、2 可跑通后再加深；**过早堆模拟成交、无数据支撑时意义很小**。

仓库里已有的 **master/agent、HTTPS 轮询注册、paper 入门**等，作为**可测占位与 CI smoke**，保留即可；**近期主线**不优先扩张这些路径。

**说明**：较早日期的 `doc/tasks/*.md` 若写「对齐 ROADMAP v1（模拟盘…）」属旧版划分，**以本文件当前表格与上文优先级为准**。

## 存储与技术栈（当前共识）

- **主存：PostgreSQL** — 行情/历史落地、元数据、任务与版本信息；用 migration 管理 schema（本地与 CI 可用容器或 `testdata` 隔离）。
- **辅存：Redis（后续）** — 缓存、队列、分布式锁、会话等**按需、分阶段引入**，不作为 v1 数据管道硬依赖；无 Redis 时单体仍可开发与测试。

## 后续阶段

| 阶段 | 目标 |
| --- | --- |
| v1 | **数据管道**：行情/历史数据拉取、**落 PostgreSQL**（迁移 + 校验）；可选仅开发用的导出目录做比对；数据源与券商侧**抽象**，单元测试可 mock，集成测再接真实凭证 |
| v2 | **回测骨架**：消费落地数据的回测运行器、时间对齐、可测的绩效/约束；仍不依赖 LLM |
| v3 | **执行路径**：订单与模拟盘/券商接口的深化，与 v1 数据域对齐；**Futu 已接入**（OpenD 网关：CLI 下单 `wbot futu order`（sim 默认、real 需 `-live-confirm`）、持仓 `position`、资金 `funds`/`ingest account` 快照，2026-08-03 实测，见 [[FUTU]]）；IBKR / Schwab 待凭证（见 [discussions/10](https://github.com/jiayuwangcj/wbot/discussions/10)）；**持仓数据 Web 化**（Futu 持仓已可读，Web 化/实盘下单 Web 化待老板拍板）；标准化日志（如 zerolog）随本阶段按需收紧 |
| v4 | **控制面与产品化**：Go API（**已提前实施**，见 [discussions/9](https://github.com/jiayuwangcj/wbot/discussions/9) 分诊）、`go:embed` Web UI、Telegram/Discord 通知；Web UI 优先形态：**PC 端 Web**（已实施：serve + 内嵌 UI；微信小程序已放弃——2026-07-31 老板决策，微信将下架含股票小程序，见 [discussions/21](https://github.com/jiayuwangcj/wbot/discussions/21)）+ **移动 Web 框架预留**；**不做看盘工具**——2026-08-02 老板决策（策略页 options 链视图已移除，`/v1/futu/options` 端点保留供数据管道/脚本用）；master/agent 运维化按需并入 |
| v5 | **决策与覆盖**：可配置 LLM 决策角色、港股/美股现货与期权等全覆盖 |

关联：[[WORKFLOW_GITHUB_DRIVEN]] [[README]] [[proposals/0001-automation-baseline]]
