# Doc Index

## 当前产品入口

- [[WHEEL_STRATEGY]]：单一动态 Wheel 的配置、库存、候选、快照和安全边界
- [[API]]：`wbot serve` HTTP 契约；`/v1/strategies`、`/v1/watchlist` 只面向 Wheel
- [[BACKTEST]]：事件回测边界、`DATA_BLOCKED` 闸门、指标和 trace
- [[tasks/2026-08-10-wheel-full-rewrite]]：替换式重构计划、阻塞登记和验收 ledger
- [[DATA_PIPELINE]]：bars、期权数据和 ingestion
- [[DATA_STANDARD]]：source、复权、时间和字段标准
- [[FUTU]]：富途 OpenD/只读数据接入
- [[ACCEPTANCE]]：verify、集成和浏览器验收总表
- [[PRIVACY]]：配置、账户数据和凭证边界

## 工程与协作

- [[WORKFLOW]]、[[TDD_WORKFLOW]]、[[FEATURE_SCOPE]]：开发与验收规则
- [[AUTO_ADVANCE]]、[[CRON_CONTINUE]]、[[CI_REPORT]]：任务推进和 CI
- [[RELEASE_DAILY]]：日构建与部署
- [[ROADMAP]]、[[PLAN_V0]]：路线与历史背景
- [`doc/issues/`](issues/)：Issue/Discussion 历史草稿（不作为当前产品契约）
- [[tasks/README]]：任务索引
- [[GITHUB_MCP]]、[[GITHUB_SETUP]]、[[WORKFLOW_GITHUB_DRIVEN]]：GitHub 驱动协作

当前主文档只描述单一 Wheel 产品；历史 issue/task 保留原文供审计，不自动升级为现行 API 契约。所有不可执行能力必须同时记录阻塞原因、启用闸门和禁止降级；没有验收证据不得标记 `READY`。
