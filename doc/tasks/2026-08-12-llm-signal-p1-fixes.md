# 2026-08-12 #35 评审 P1 修复(cursor 水线 + 拒绝推送)

- **id**: `2026-08-12-llm-signal-p1-fixes`
- **created**: `2026-08-12`
- **parent**: #35(LLM 策略注入端点 + 全流程闭环 + 多模拟账户自动解析)

## Goal

修复 reviewer(2026-08-12)对 feat/llm-signal-endpoint 合入候选的两个 P1 发现,收口 #35 合入。

## P1-2:runPush cursor 推进可跳过后置 pending 信号(推送静默丢失)

- **位置**:cmd/wbot/telegram_scheduler.go:246-249 + wheelstore QuerySignalsSince
- **问题**:同批 signals(id 升序)中前一个 retry(pending)后一个成功时,`cursor = sig.ID` 越过前者;其审核落地后 id>cursor 永不再查,APPROVE 也永不推送;serve 重启以 MaxSignalID 起步也不回补。
- **失败场景**:批量注入 2+ 信号且 poll 恰落在首个信号 AppendSignal 与 AppendAction 之间(LLM 审核 1-5s 窗口,30s poll 命中概率低但后果是静默丢失老板必需的推送)。
- **修法**:cursor 改水线语义(仅当 (oldCursor, sig.ID] 区间无 pending 时才前进)+ 内存已推集合(或记 PUSHED action)防重推;补「前 pending 后成功同批」回归测试。

## P1-1:「LLM 拒绝理由推送」是死代码,老板指令未交付

- **位置**:cmd/wbot/telegram_scheduler.go:280-303(reviewer 引用行号,以实际为准)
- **问题**:两个链路 REJECT 均记录 action="REJECTED"(wheelrun runner.go:279 / llm_signal.go reviewLLMSignal),而 LatestLLMReview 只查 action='LLM_REVIEW'(wheelstore/store.go:758-772),LLM_REVIEW 仅在 APPROVE 时写入 → `verdictOf(review)!="APPROVE"` 分支永不可达。REJECTED 信号走 280-286 静默跳过(仅日志),老板收不到拒绝理由推送;TestPushSignalSkipsRejectedReview 还固化了不推送。
- **失败场景**:信号 269/270/271 类 REJECT 时老板无任何推送,无法了解策略为何失败。
- **修法**:ErrNotFound+HasAction(REJECTED) 分支改为取 REJECTED action 的 reasons 并 sendToChats 推送;补断言推送的测试(替换/补充 TestPushSignalSkipsRejectedReview)。

## Constraints

- 所有策略仅限价单(price>0 fail-closed);推送必须带信号/订单编号。
- 测试 fixture 用假值;敏感配置不进仓库。
- 提交署名按实际编写模型;verify.sh 全绿才提交。

## Links

- 评审报告:doc/tasks/2026-08-12-review-plan.md「评审结论」小节(P1-1/P1-2)
- PR: feat/llm-signal-endpoint(合入中,此分支修完后并入)

## State

- **status**: `done`
- **last step**: 2026-08-12 已合入 feat/llm-signal-endpoint(f43e6ff)
- **coder**: codex(gpt-5.6-luna,commit 74d31b3)
- **verify**: scripts/verify.sh 全绿(go test 全包 ok)
- **reviewer 复评**(2026-08-12):无条件合入,bugfix,无 P0/P1;P2 建议「integration_test 补 LatestAction(REJECTED) 正路径断言」不阻断;P3 观察「重启跨审核窗口丢推送需持久化 PUSHED」列入 backlog 评估

## Next

- (backlog)重启跨审核窗口丢推送 → 持久化 PUSHED action/推送游标表(替代内存 handled 集合)
- (backlog)integration_test 补 LatestAction(REJECTED) 正路径 PG 断言
