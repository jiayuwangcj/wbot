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

## 实时 Wheel 部署环境

启动实时评估使用 `wbot serve -wheel-run`；`-wheel-interval` 控制轮询周期，`-wheel-env sim|real` 选择账户环境。行情与账户读取分别使用 `$FUTU_GATEWAY_URL`、`$FUTU_PROTO_ADDR`。

LLM 审核闸门需要同时设置以下环境变量：

```bash
export LLM_BASE_URL='https://llm.example/v1'
export LLM_API_KEY="$SECRET_LLM_API_KEY"
export LLM_MODEL='审核模型名'
```

`LLM_BASE_URL` 应指向 OpenAI-compatible API base URL，客户端会请求 `/chat/completions`。任一变量缺失时，开启 Wheel 的 serve 会打印 warning，并保持 ALERT 不推送；key 只从环境变量读取，不落库、不打印。

需启用 Telegram 人工处置时，另加 `-telegram-run`，并在 `~/.wbot/wbot.conf` 配置 `credentials.telegram.token` 与 `credentials.telegram.chat_ids`。Telegram 只接收 LLM `APPROVE` 的 ALERT；`yes` 仅允许模拟环境下单，`no` 和“今日不再提醒”均写入审计记录。受控测试可用 `WBOT_CONFIG_DIR` 与 `TELEGRAM_API_BASE_URL` 指向临时配置和 fake endpoint。

## 工程与协作

- [[WORKFLOW]]、[[TDD_WORKFLOW]]、[[FEATURE_SCOPE]]：开发与验收规则
- [[AUTO_ADVANCE]]、[[CRON_CONTINUE]]、[[CI_REPORT]]：任务推进和 CI
- [[RELEASE_DAILY]]：日构建与部署
- [[ROADMAP]]、[[PLAN_V0]]：路线与历史背景
- [`doc/issues/`](issues/)：Issue/Discussion 历史草稿（不作为当前产品契约）
- [[tasks/README]]：任务索引
- [[GITHUB_MCP]]、[[GITHUB_SETUP]]、[[WORKFLOW_GITHUB_DRIVEN]]：GitHub 驱动协作

当前主文档只描述单一 Wheel 产品；历史 issue/task 保留原文供审计，不自动升级为现行 API 契约。所有不可执行能力必须同时记录阻塞原因、启用闸门和禁止降级；没有验收证据不得标记 `READY`。
