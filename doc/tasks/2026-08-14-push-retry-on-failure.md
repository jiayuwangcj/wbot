# 推送瞬时失败重试:Discord/Telegram 卡片失败即永久丢失(769 实测)

**State**: 派单中——Claude 侧 coder 独立 worktree(2026-08-14,codex 被 #61 占用,文件不重叠故不冲突)

## Goal

769(2026-08-14 16:55:28 APPROVE)实测发现:Discord/Telegram 推送瞬时失败即**永久丢失**,无重试。老板收不到 APPROVE 卡片 = 无法下单决策,资金安全关键路径。修复为与 LLM review 未记录一致的 retry 语义(保持游标,下轮 30s 重推)。

## 实测事实(2026-08-14,serve 日志)

- **769 推送丢失链**:769 创建 → 审核前 telegram+discord 双通道 6 次「LLM review not yet recorded, will retry」(游标保持)→ 16:55:28 审核 APPROVE 记录 → 16:56:11 `discord: push: US.JD signal=769: discord: create message: request failed` → **之后无重试,游标推进,769 卡片永久丢失**
- **根因(discord_scheduler.go:284-286)**:`pushSignalDiscord` 中 `pushEmbedDiscord` 失败只 logf 后 `return false` → 推送循环标记 handled + 推进游标。与注释声称的「a later signal never jumps a retryable prefix」语义不一致——LLM review 未记录是 retryable(返回 true 保持游标),推送失败却是 fail-fast 永久跳过
- **telegram 侧同病(telegram_scheduler.go:407-409)**:`SendMessage` 失败 logf 后同样 `return false` 永久跳过。769 这次 telegram 恰成功(无失败日志),但同 bug 存在
- 768 REJECTED 推送(5c58192 下)成功;770 已创建(17:00 前后),待审核——若 Discord API 持续抖动,770+ 会继续丢
- **⚠️ 新观察(17:01)**:770 审核 16:59:48 落库(REJECTED),telegram 16:59:57 推 REJECTED 卡,但 **discord 循环 16:59:27 后零日志**(16:59:57/17:00:27/17:00:57 三个 30s tick 全静默)。769 失败后循环曾正常(770 retry 到 16:59:27),故非 CreateMessage 15s 超时卡(且 logf 在 CreateMessage 前);PG 无 active query;telegram 同 DB 正常。**discord 推送循环疑似死亡(goroutine 卡死/退出),不可观测**(serve 无 pprof、循环无心跳日志、无 panic recover)。待 771(17:04-17:05 评估)验证:771 ALERT 后 telegram retry 而 discord 静默 → 确认死

## 改动面(仅 2 文件 + 单测,与 #61 backtest-p0 worktree 不重叠)

- **cmd/wbot/discord_scheduler.go**:
  - `pushSignalDiscord` APPROVE 分支:`pushEmbedDiscord` 失败 → logf → `return true`(retry,保持游标,30s 后重推)
  - `pushRejectedDiscord`(501 行 `_ = s.pushEmbedDiscord`):失败补 logf;改签名返回 retry bool,失败重试(REJECTED 卡片丢失同样不可静默)
- **cmd/wbot/telegram_scheduler.go**:`pushSignal` APPROVE 分支 `SendMessage` 失败 → 改为 `return true`(当前 return false)
- **单测**:模拟 CreateMessage/SendMessage 失败 → 返回 true;成功后返回 false;dismissed/REJECTED/freshness 过期仍永久跳过(不回归)

## Constraints

- 资金安全铁律:不引入自动下单;本修复只影响推送重试语义
- 与 LLM review 未记录同语义:retryable 与 permanent 的边界不混淆(REJECTED/dismissed/过期 = permanent)
- 不碰 wheel.go/runner.go/store.go(#61 与老板并发开发区域,先 grep 确认)
- 编码执行:codex 单飞中(#61),按并行协议 Claude 侧 coder 可与 codex 并发(worktree/文件不重叠)

## Verify(验收)

- gofmt/vet/test/race/staticcheck + scripts/verify.sh 全绿
- 单测断言:推送失败 → pushSignalDiscord/pushSignal 返回 true;重试成功 → false;REJECTED/dismissed 不回归
- 端到端(合入部署后):推送失败日志后 30s 内出现重试日志,卡片最终送达

## Links

- 任务: #62(本任务);相关 #60(769 链路根因分析见 doc/tasks/2026-08-14-order-stub-watchfill.md)
- 代码: cmd/wbot/discord_scheduler.go:284-286(APPROVE 失败永久跳过)、:501(REJECTED `_ =`);cmd/wbot/telegram_scheduler.go:407-409

## 实测状态更新(2026-08-14 01:15 北京)

- **#62 评审(17:11)**:有条件合入——P0 无;P1-1(telegram 多 chat 部分失败 → 每 30s 向成功 chat 重复推下单卡,confirmOrder 无去重)由 coder 修复中;P2×4(重试退避/升级、goroutine recover、telegram REJECTED 卡 retry、query 错误路径心跳)排期;P3×3 观察
- **deepseek 时段性不稳定确认**:769(16:55:28)/770(16:59:48)审核成功(输入含 4 条 stub pending,34KB)→ **17:05 起连续失败**:771 两次(17:05:30/17:08:33,均 180s 超时)+ 772 第一次(17:11:55,重试中)——与凌晨 00:30-00:40 超时段(762/763/764)同模式。**同一输入规模 16:55 成功 17:05 失败 → 主因 deepseek 凌晨时段不稳定,pending 膨胀(4 条 stub)是放大器**
- **存量 pending 确认**:744/757/761/765 四条 CONFIRM 全部 stub(order_id=206158430256,order_id_ex="0"),合约 P29000/P28500,14:50-16:35;清理仍等老板拍板(补 REJECTED 标记即出 pending,留痕不删)
- 771/772 审核失败 → 双通道 skip,信号丢失(与 762/763/764 同类);#63 竞态修复待 #62 合入后派生

## 合入完成(2026-08-14 01:17 北京)

- **15d0a9a merge(fix)**:#62 合入主基线(feat/llm-signal-endpoint,5c58192 之上);评审「有条件合入」条件 P1-1(telegram 部分失败重复推)已由 coder 修复(1641596:per-signal delivered 集合,只补发失败 chat,2 新测覆盖);P2×4 排期(重试退避/升级、goroutine recover、telegram REJECTED retry、query 路径心跳)、P3×3 观察
- **待部署**:bugfix 可及时发(发布机制);serve 当前镜像 7719de50(5c58192),合入后新镜像待发布
- 部署后验收:① push 失败 → 30s 内重试日志 + 心跳每 2.5 分钟一行 ② 769 同类瞬时失败不再丢卡

## Next

- 部署 15d0a9a(operator/老板);部署后观察心跳与重试
- #63(竞态修复)从 15d0a9a 派生派单
