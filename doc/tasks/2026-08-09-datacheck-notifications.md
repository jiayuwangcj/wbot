# datacheck 异常外部通知

- **id**: `2026-08-09-datacheck-notifications`
- **created**: `2026-08-09`
- **updated**: `2026-08-09`

## Goal

每日 datacheck 自动 repair 后仍存在缺失/过期项，或调度本身失败时，可选发送 Telegram/Discord 摘要；让无人值守部署无需持续查看 stderr。

## Constraints

- 默认关闭；未配置任何凭证时 `wbot serve` 行为不变。
- 仅集成到 serve 内置调度，不为浏览器增加 repair/notify 写端点。
- token、chat id、webhook URL 不写日志、不进入错误正文；HTTP 响应体不回显。
- 一个通知通道失败不阻断另一个通道，也不改变 repair 结果。
- 使用 `httptest` 验证请求方法、路径和 JSON；不调用真实外部服务。

## Links

- Depends-On: `2026-08-09-datacheck-observability`
- Planned-By: `2026-08-09-datacheck-priority-plan`

## State

- **status**: `done`
- **last step**: 已实现 Telegram/Discord adapter、显式 serve 开关、多通道容错、脱敏错误与异常摘要测试。

## Next

进入 Data 页最终易用性复核与全链路真实环境验收。

## Decision / Evidence

- `-datacheck-notify` 默认 false；未开启时不读取任何通知环境变量。
- Telegram 使用 `DATACHECK_TELEGRAM_BOT_TOKEN` + `DATACHECK_TELEGRAM_CHAT_ID`；Discord 使用 `DATACHECK_DISCORD_WEBHOOK_URL`；允许同时配置。
- 只发送调度错误、repair 错误或 repair 后仍异常的摘要；完整且无错误时静默。
- 消息最多列出 10 个异常项并限制 1800 字符，兼容 Discord 内容上限且避免超长告警。
- HTTP 错误不包装底层 URL、不读取/回显响应体；adapter 错误不包含 token、chat id、webhook。
- `go test ./internal/notify ./cmd/wbot -count=1`: PASS。
