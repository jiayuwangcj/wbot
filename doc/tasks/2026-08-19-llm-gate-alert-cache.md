# LLM 审核门：失败/欠费 Discord 运营告警 + 缓存友好与命中率统计（2026-08-19）

## Goal

解决生产 LLM 审核门的两个运营缺陷（08-18 欠费事故复盘，证据见 `wheel_signal_actions` 132 次 `LLM_REVIEW_FAILED`）：

1. **失败/欠费/退化不得静默吞掉**：LLM 审核失败（HTTP 402 欠费、5xx、超时等）目前只在 stderr 打日志 + `pushSignalDiscord` 静默 skip——用户看不到，人工处置链路断了也无感知。改为失败超窗后向 **Discord 推送运营告警 embed**（含 symbol/signal/错误原因/时间/影响），同类错误节流防刷屏。
2. **缓存友好 + 命中率统计**：当前 `userContent` 用 `map` 构建 + `json.Marshal`（按键排序），动态字段（signal/positions/pending_orders）与静态字段（rules/strategy_config）交错，DeepSeek context caching 前缀不稳定 → 命中率低、input 成本高。改为**静态前缀在前**的有序 JSON；解析响应 `usage.prompt_cache_hit_tokens / prompt_cache_miss_tokens`，把 usage 与命中率写入 gate 的 action `details` 落库，可 SQL 统计。

改动向后兼容：不改 ReviewRequest 外部契约、不改 verdict 语义、不加新配置键。

## 背景（2026-08-18/19 生产事故）

- deepseek 账户欠费（HTTP 402 Insufficient Balance）→ 全部信号审核失败 → fail-closed 跳过推送。08-18 单日 132 次 `LLM_REVIEW_FAILED`，signal 1252 超窗永久跳过、1254/1256 每轮重试。
- 消耗账目：US.JD 配置 v4（08-14 15:16）`min_premium_per_share` 1.53→0.1、`min_option_profit` 200→13 → 每小时 5-12 次 ALERT → 每天 ~55 次审核调用（正常期 ~103 次/天），每次请求体平均 55.4KB ≈ 15-20k input tokens。调用量暴涨 + 欠费后重试放大 ×2 → 余额 08-18 耗尽。
- **本任务只做通用防御（告警 + 缓存），不做 US.JD 配置回调**（那是配置决策，另议）。

## Constraints

- 向后兼容：`ReviewRequest`/`ReviewResult` 不破坏性变更（可加字段）；verdict/disposition 语义不变；现有 watchlist/wheel_configs 不受影响
- **告警必须节流**：欠费是全局性问题，会在每个失败信号上触发——同类错误（按 status/错误类别）冷却窗口内最多推一条，避免告警风暴
- 告警只走 Discord（用户指定）；telegram 推送器不动
- 提交前 `scripts/verify.sh` 全绿；新逻辑补单测（前缀稳定/usage 解析/告警节流/告警落发）
- worktree: `.claude/worktrees/llm-gate-alert-cache`（分支 `fix/llm-gate-alert-cache`）
- 提交署名 `Co-Authored-By: Claude <noreply@anthropic.com>`（codex 额度耗尽,退回 Claude 侧 coder 执行）

## 改动设计

### 1. `internal/llmreview/llmreview.go` — 缓存友好 + usage 解析

**userContent 稳定前缀**：现实现（174-212 行）用 `map[string]any` + `json.Marshal` 按键排序，无法控制顺序。改为**显式有序字段**：

- 前缀（近静态，缓存共享）：`rules`（全局静态文本）→ `strategy_config`（per-symbol 版本稳定）→ `symbol` → `current_date`
- 后缀（每次变化）：`current_price` → `cash_available` → `inventory` → `positions` → `pending_orders` → `observed_options` → `signal`

实现：构建有序 `[][]byte`（field key + value raw），value 逐个 `json.Marshal` + `dropZeroTimes` 清洗，再按序拼 `{...}`（`json.MarshalIndent` 2 空格，确定性输出）。**保持现有 `dropZeroTimes` 语义**（zero time 剔除，signal 454/455 教训）。

**响应 usage 解析**：`chatCompletion` 加 `Usage` 字段：
```go
type usageInfo struct {
    PromptTokens            int `json:"prompt_tokens"`
    CompletionTokens        int `json:"completion_tokens"`
    TotalTokens             int `json:"total_tokens"`
    PromptCacheHitTokens    int `json:"prompt_cache_hit_tokens"`    // DeepSeek context caching
    PromptCacheMissTokens   int `json:"prompt_cache_miss_tokens"`
    PromptTokensDetails     *struct{ CachedTokens int `json:"cached_tokens"` } `json:"prompt_tokens_details"` // OpenAI 兼容
}
```
`ReviewResult` 加 `Usage Usage`（或 `CacheHitTokens/CacheMissTokens` 两字段），`Review` 成功路径填充（`prompt_cache_hit_tokens` 缺失时回退 `prompt_tokens_details.cached_tokens`）。

### 2. `internal/llmreview/gate.go` — usage 落库

`RecordLLMGate` 在**成功审核**分支（`result` 有效）把 usage 写入 `details["usage"]`：
```json
{"prompt_tokens":N,"completion_tokens":M,"cache_hit_tokens":H,"cache_miss_tokens":K,"cache_hit_rate":0.83}
```
`cache_hit_rate = H/(H+K)`，H+K=0 时为 `null`。失败分支无 usage（402/5xx 不返回）。命中率可 SQL 聚合查询。

### 3. `cmd/wbot/discord_scheduler.go` — 失败运营告警

`pushSignalDiscord` 的 `LLM review failed beyond retry window; skip push` 分支（276-277 行）改为：**跳过卡片推送 + 发运营告警 embed**（scheduler 已有 `dc *discord.Client` 和 `channelID`）。

- **告警内容**（embed）：`⚠️ LLM 审核门故障` 标题；字段：symbol、signal ID、错误原因（从 `LatestAction(LLM_REVIEW_FAILED).Details["error"]` 提取）、失败时间、影响（"信号推送被跳过，人工处置链路中断"）。正常 ALERT 卡片仍走 `signalDiscordEmbeds` 逻辑，告警是独立 embed。
- **错误类别**：从 error 字符串提取稳定类别——`status 402`→`"402"`、`status 5xx`→`"5xx"`、`timeout`/`deadline`→`"timeout"`、`unexpected LLM verdict`→`"verdict"`、其他→`"other"`。
- **节流**：scheduler 加 `alertMu sync.Mutex` + `lastAlert map[string]time.Time`（key = 错误类别）；同类错误 `llmAlertCooldown`（建议 30min）内不重复推。测试可缩短常量或注入。
- **告警发送失败**：只打日志，不影响主推送游标语义（告警是尽力而为，卡片跳过逻辑不变）。

### 4. 单测

- `llmreview_test.go`：userContent 前缀稳定性（两次调用同 symbol 前缀字节一致；rules/strategy_config 在前）；usage 解析（DeepSeek hit/miss 字段 + OpenAI cached_tokens 回退）；ReviewResult 携带 usage
- `gate_test.go`：成功审核时 details 含 usage/cache_hit_rate；H+K=0 时 hit_rate null
- `discord_scheduler_test.go`：失败超窗 → 发告警 embed 且字段完整；同类错误冷却窗口内不重复；错误类别提取

## Links

- 失败门控：`cmd/wbot/discord_scheduler.go:234-285`（pushSignalDiscord + skip 分支）；`cmd/wbot/telegram_scheduler.go`（同语义，本任务不动）
- LLM 客户端：`internal/llmreview/llmreview.go`（userContent 174-212、chatCompletion 166-172、Review 124-164）；`internal/llmreview/gate.go`（RecordLLMGate 26-99）
- 审核 worker 失败路径：`internal/wheelrun/runner.go:636-710`（reviewAlert、重试、clearSuppression）
- 事故证据：`wheel_signal_actions`（192.168.215.2:5432/wbot_test），08-18 132 次 `LLM_REVIEW_FAILED`，details.error = "status 402 Payment Required: Insufficient Balance"
- 消耗账目：LLM 请求体平均 55.4KB（DB details 实测）；DeepSeek context caching 命中 input 单价约为未命中 1/4（按 provider 定价）

## State

- [x] 任务记录 + 根因分析（08-19 主会话：402 欠费、US.JD 门槛过松、缓存前缀不稳定）
- [x] coder/codex 实现 + verify.sh 全绿
- [ ] reviewer 评审 + 合入 main
- [ ] 重建 serve 镜像部署（合入后），欠费事故恢复后验证告警推送 + 缓存命中率可见

## Next

- 本任务合入后：观察 `wheel_signal_actions.details.usage` 的 cache_hit_rate（SQL 聚合），确认缓存生效
- US.JD 门槛回调（min_premium_per_share/min_option_profit 恢复合理值）——配置决策待老板裁决
- deepseek 欠费充值后：审核门自动恢复，验证告警在恢复后不再触发
