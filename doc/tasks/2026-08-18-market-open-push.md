# 开盘准备状态推送（2026-08-18 老板指令）

## Goal

serve 在**三个时机**推送「开盘准备状态」到 Telegram，让老板知道服务准备好了、当前在自动 watch 什么：

1. **serve 启动后立即推一次**（跟随 serve 启动，确认服务起来了）
2. **HK 开盘前**（默认 09:15 Asia/Shanghai）推一次
3. **US 开盘前**（默认 21:15 Asia/Shanghai = ET 盘前）推一次

推送内容（老板 2026-08-18 AskUserQuestion 确认）：

- **连接情况**：futu 网关、DB、LLM 审核闸门等关键依赖是否可用
- **watch 标的清单**：当前 watchlist 里正在自动 watch 的标的（symbol + strategy + 参数状态 + 执行状态）
- **账户/资金摘要**：模拟盘 env 资金快照（可用资金/持仓/净值）
- **服务集群情况**：单进程组件视图（复用 `GET /v1/admin/cluster` 语义）

## 背景（2026-08-18 诊断，触发本任务）

老板反馈「HK.00700 白天没有推送了」。诊断：
- HK.00700 不在 watchlist（最后信号 2026-08-14 DATA_BLOCKED）→ 已加回 watchlist ✓（主会话 2026-08-18 执行）
- serve 容器跑 8-14 旧镜像，不识别 8-15 新增的 wheel 参数 `covered_call_pct`（commit 8c84567）→ ACCEPT.US 与 HK.00700 配置解析报错 → 镜像重建中（主会话）

「开盘准备状态推送」为新功能（用户确认）：不仅回答「现在在 watch 什么」，还让用户知道服务状态。

## Constraints

- 通知走现有 Telegram 凭据：`~/.wbot/wbot.conf` 的 `credentials.telegram.token` / `credentials.telegram.chat_ids`（复用 `cmd/wbot/telegram_scheduler.go` 的 `openTelegramConfig` 模式）；**不新增敏感配置**；未配置时 log once + 静默跳过（与 `startTelegramScheduler` 一致）
- 复用现有调度/通知基建：`datacheck.RunDaily(ctx, hour, minute, task)`（`internal/datacheck/service.go:97`）、`notify.Sender`
- 不污染既有推送路径（wheel ALERT、datacheck、discord）；实时 wheelrun 路径零改动
- 推送失败不 crash serve（log stderr，与既有 scheduler 一致）
- 提交前 `scripts/verify.sh` 全绿；关键路径补单测（组装函数）
- worktree: `.claude/worktrees/market-open-push`（分支 `feature/market-open-push`，基于 origin/main=8d90b62）
- 提交署名 `Co-Authored-By: Claude <noreply@anthropic.com>`

## Links

- 复用模式：`cmd/wbot/datacheck_scheduler.go`（`RunDaily` + notifier）、`cmd/wbot/telegram_scheduler.go:213`（wbot.conf telegram 凭据）
- 集群视图：`internal/httpapi/admin_cluster.go`（`processJSON` 组件视图）
- 账户资金：`internal/futu/trade.go:345`（`TradeClient.Funds`，sim env 只读）、`internal/httpapi/account_snapshots.go`
- watchlist：`internal/httpapi/DBWatchlistStore` / `wheelstore`（symbol/strategy/params/config_version/执行状态）
- 网关连接：`resolveFutuGateway`（`cmd/wbot/`）；DB：`database.PingContext`

## 改动面（建议）

| 文件 | 改动 |
| --- | --- |
| `cmd/wbot/market_open_scheduler.go`（新） | `startMarketOpenScheduler(ctx, database, env, hkAt, usAt)`：serve 启动即推 + `RunDaily` 两个开盘前时机 |
| `cmd/wbot/main.go` | serve 加 flags `-prep-at-hk 09:15` / `-prep-at-us 21:15`（默认值可配）+ wiring；复用 wbot.conf telegram 凭据 |
| `cmd/wbot/market_open_scheduler_test.go`（新） | 组装/文本生成单测（不含外部依赖） |

## State

- [x] 任务记录（主会话 2026-08-18）
- [ ] coder 实现（新 worktree `market-open-push`，分支 `feature/market-open-push`）
- [ ] verify 全绿 + 单测补全
- [ ] reviewer 评审 + 合入 main
- [ ] 部署：serve 镜像重建含新功能（与 covered_call_pct 修复一起二次部署）

## Next

- [ ] 主会话派 coder 实现 → verify → reviewer 评审（feature）→ 合入 main
- [ ] 主会话在 serve 镜像重建完成后：验证 HK.00700 恢复被监视 + ACCEPT.US 配置不再报错
