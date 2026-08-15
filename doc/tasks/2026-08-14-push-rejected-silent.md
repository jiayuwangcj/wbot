# 推送器:LLM REJECTED 静默不推(2026-08-14 老板指令:「LLM 评审决定是否推送,重复推送不通过的消息太多」)

## Goal

用户在 Discord 收到大量「不通过」(LLM REJECTED)推送卡——同一 PUT gap 每 5 分钟一信号,LLM 多数拒掉,但推送器对 REJECTED 也推「pushing reasons」卡,一天几十张噪声。修复:LLM 审核决定是否推送——**只有 APPROVE 推送(走 confirmOrder),REJECTED/其他裁决静默推进游标**(DB 审计保留,不推卡)。

## 改动规格

1. `cmd/wbot/telegram_scheduler.go` pushSignal:
   - REJECTED 分支(约 389-398 行):不再推送;游标推进(return false 语义不变);日志降频为一行「LLM review REJECTED; silent skip」(原「pushing reasons」逻辑移除)
   - 非 APPROVE 分支(约 432-434 行 verdictOf != APPROVE):同样静默推进,不推卡
   - 保留:FAILED 窗口重试(#63)、freshness、dismissed、APPROVE 推送+confirmOrder、DB 审计落库(REJECTED 已在审核阶段落库,静默不丢审计)
2. `cmd/wbot/discord_scheduler.go` pushSignalDiscord:同结构同步修改
3. 测试:`telegram_scheduler_test.go` / `discord_scheduler_test.go` 中 REJECTED 断言从 send=1 改为 send=0 + 游标推进;新增 APPROVE→send=1 回归;FAILED 窗口测试不受影响
4. verify 全绿

## Constraints

- worktree: `.claude/worktrees/push-rejected-silent`(分支 fix/push-rejected-silent,基于主基线)
- 提交前 scripts/verify.sh 全绿;署名按实际编写模型
- 不碰 #63 窗口逻辑与 APPROVE 下单路径(资金安全:APPROVE 推送必须保留)
- REJECTED 静默后,用户仅收到 APPROVE(需人工确认下单)与确认回调消息

## Links

- 用户反馈:2026-08-14「重复推送不通过的消息太多」
- 推送器现况:#62(失败重试)/#63(FAILED 窗口)已合入;本片只改裁决推送分支
- 数据实证:774-805 信号流(多数 REJECTED,推送器全量推卡)

## State

- [ ] telegram REJECTED 静默
- [ ] discord REJECTED 静默
- [ ] 测试更新 + verify 全绿

## Next

coder 开发(subagent 与 #67 codex 并发,文件不重叠)→ 主会话评审 → 合入 → 部署后 00700/信号流噪声消失。

## 收口(2026-08-14 主会话)

- coder 292201f → reviewer 评审**无条件合入**(feature,回归 WHEEL_STRATEGY.md:104-105 契约:只有 APPROVE 才推)
- 合入 d3eac99(--no-ff),已 push;serve 重建部署,health 绿
- 部署后实证:signal 824 REJECTED →「LLM review REJECTED; silent skip」(telegram+discord),无推卡,游标推进 pending=false;「not yet recorded, will retry」保留
- P2 排期:审核失败可观测性(REJECTED 降噪后 LLM 故障在 IM 无感知,排「失败汇总推送」);P3:verdict 空形态测试、FAILED+REJECTED 双 action 顺序测试
