# 推送器 FAILED skip 与 LLM gate 重试竞态:审核重试成功也丢信号(771 实测)

**State**: 已合入主基线(7febed3),待部署

## Goal

LLM gate 失败重试(runner.go:385「retrying once」)与推送器 FAILED skip(discord_scheduler.go:263-265 / telegram 同)竞态:**第一次审核失败落 LLM_REVIEW_FAILED → 推送器下一 tick(≤30s)读到 FAILED 永久 skip + 推进游标 → gate 的同步重试(3s 后开始,180s 窗口)即使成功,审核记录落库但信号已被 skip,白推**。一次超时 = 信号必丢,重试承诺失效。

## 实测事实(2026-08-14)

- **771 链**:评估 17:02:18 开始 → 17:05:30 `wheelrun: US.JD: LLM gate transient failure signal=771, retrying once: <nil>`(第一次失败落 FAILED)→ 重试 17:05:33 开始(180s 窗口)→ **17:05:57 推送器双通道「LLM review failed (transient); skip push」**(FAILED 已存在)→ 771 永久 skip → **17:08:33 重试也超时(第二条 LLM_REVIEW_FAILED,180s 整点)** → 771 双通道丢失。本次重试失败,竞态未触发,但竞态窗口被跳过(推送器 skip 时重试确在途)
- **764 竞态实锤(历史)**:16:31:30 第一次失败 → 重试 16:33:57 **成功落审核记录** → 但 764 已被推送器 skip(FAILED 存在即 skip)→ 审核成功仍丢。竞态造成的真实丢失案例
- **772 竞态实锤(17:14)**:17:11:55 第一次失败落 FAILED → 推送器 17:11:57 skip(30s tick 内)→ **重试 17:14:25 成功落 REJECTED(147s 窗口内,deepseek 恢复)** → 但 772 已 skip,卡片不推。审核成功被吞,竞态第二次实证
- **代码确认**:runner.go:361-382 gate() → RecordLLMGate 失败即落 LLM_REVIEW_FAILED action(注释「RecordLLMGate 落库后返回 nil err,失败语义经 disposition 传递」);runner.go:385-397 重试在 3s 后(重试成功落正常审核记录,重试失败落第二条 FAILED);推送器(discord_scheduler.go:257-265)HasAction("LLM_REVIEW_FAILED") → skip + return false(永久)
- **判断顺序**:推送器 LatestLLMReview 优先 → 若重试成功落审核记录,**之后**的 tick 会走正常分支——但 skip 发生在重试完成前(30s tick vs 180s 窗口),先到先得,审核记录救不回已 skip 的信号
- **deepseek 稳定性**:771 两次调用均 180s 超时;769(16:55:28)/770(16:59:48)同输入规模成功——审核输入含 4 条 stub pending(744/757/761/765),膨胀压力持续,待 772 观察

## 修复方案(已实施方案 A)

- **方案 A(推送器侧,最小,已实现)**:FAILED action 加新鲜度判断——`LatestAction(ctx, sig.ID, "LLM_REVIEW_FAILED")` 取 created_at,距今 < 窗口(5min)→ 返回 retry(游标保持,下轮复查);≥ 窗口仍无审核记录 → skip。重试成功则 LatestLLMReview 优先正常推送 ✓;重试失败(第二条 FAILED)窗口内 retry、窗口外 skip ✓
- 实现细节:telegram_scheduler.go const 块新增共享常数 `llmReviewRetryWindow = 5 * time.Minute`(注释与 runner.go 3s sleep + 180s http.Client 超时互指);discord_scheduler.go 同结构同步改,共享同一常数;判断顺序 LatestLLMReview(正常分支)→ REJECTED → FAILED(窗口判断);HasAction/LatestAction 查询错误 return true 保守不丢卡
- 方案 B(runner 侧延迟落 FAILED):改 RecordLLMGate 语义,影响面大(llmstrategy 共用),不采用

## Constraints

- 与 #62 同文件(discord_scheduler.go/telegram_scheduler.go 的 pushSignalDiscord 判断区),须等 #62(997effe)合入后从新基线派生,顺序合入
- 不碰 runner.go(#61 codex 区域;且方案 A 不动 runner)
- 推送语义边界不变:审核 REJECTED/dismissed/过期仍 permanent

## Verify(验收)

- 单测:FAILED 新鲜(窗口内)→ retry=true;FAILED 陈旧(窗口外)→ false;重试成功落审核记录后正常推送(不 skip)
- 端到端(合入部署后):gate 失败日志后,推送器打「retrying」而非「skip」;重试成功则卡片送达

## Links

- 任务: #63(本任务);相关 #62(推送失败重试)、#60(769 链路)、#56(LLM 审核失败重试——本竞态的引入点)
- 代码: internal/wheelrun/runner.go:361-397(gate 重试);cmd/wbot/discord_scheduler.go:257-265(FAILED skip);cmd/wbot/telegram_scheduler.go(同结构)

## 修复完成(2026-08-14 01:2x 北京)

- **coder 完成(commit a426810,分支 fix/gate-retry-race,基线 15d0a9a)**:方案 A 实现,4 文件 +176/-9(telegram/discord_scheduler.go 各 +21/+28 行判断,测试各 +68);verify.sh 全绿(含 gofmt/test/vet/race/staticcheck/交叉编译/CLI smoke);6 新单测:FAILED 窗口内 retry=true 无发送、窗口外(6min)retry=false、FAILED+重试成功落 LLM_REVIEW → 正常推送 send=1(772 丢卡回归验收);已 push origin
- **774 实证(17:21Z)**:新部署 15d0a9a 后首轮完整评估成功(ALERT capability=READY signal=774)——deepseek 凌晨不稳定时段已恢复,推送链路正常;774 无失败无竞态窗口
- 竞态已两次实锤(764/772)且 #63 修复未部署前若再遇 gate 失败仍会丢——合入部署优先级 bugfix

## 评审结论(2026-08-14 01:3x 北京,reviewer)

**有条件合入(功能类型:bugfix)**——机制设计正确、双通道一致、单测覆盖核心路径(状态式模拟竞态,非顺序调用)、CI 可达;条件 = P1-1:

- **[P1-1 合入条件]窗口与 300s 审核超时失配**:合入目标分支已含 97e769b(llmreview.go:78 http.Client Timeout 180s→300s),coder 基线 15d0a9a 假设 180s。重试最长时间 = 3s + 300s = 303s > 窗口 300s——尾 3s 竞态存活(771/772 打满超时是实测形态)。修复:窗口 ≥306s(改 6min)+ 注释耦合点指向 llmreview.go:78,不再写死「180s」→ 已派回 coder(01:3x)
- **P2-1(建议紧随)**:runner 只重试一次,第二次失败即永久失败,但 LatestAction 取最新条 → 第二条 FAILED 把窗口重算 → 死信号最多占游标 ~483s + 误导性「will re-check」日志。建议 FAILED 计数 ≥2 即时 skip 或窗口从首条 FAILED 起算(非正确性 bug,结局同为 skip 不误推)
- **P2-2(日志噪声)**:窗口内「will re-check」每 30s × 每信号 × 双通道无节流,LLM 故障期 4N 行/分钟 ≤5-8 分钟;建议按信号降频
- **P3×3**:gate.go:47-51 注释漂移(「failed 跳过推送推进游标」已不准确)、缺 FAILED+REJECTED 验收测试(772 原景是 REJECTED 非 APPROVE)、缺 5min 边界与双 FAILED 测试
- 已验证:重启孤儿不受影响(游标 MaxSignalID 起,窗口机制只在单进程生命周期内生效,无回归);错误路径 HasAction/LatestAction 失败 return true 保守保卡;REJECTED/dismissed 判断在 FAILED 之前不破坏 permanent 语义

## 合入完成(2026-08-14 01:4x 北京)

- **P1-1 修复(2410d75,amend)**:窗口 5→6min(360s ≥ 3s+300s=303s,余量 57s);注释耦合点改指 llmreview.go:78(300s 超时,97e769b),不再写死 180s;陈旧测试 6min→7min(避免 `<` 边界巧合);verify.sh 全绿
- **7febed3 merge(fix)**:#63 合入主基线(feat/llm-signal-endpoint,97e769b 之上);评审「有条件合入」条件 P1-1 已满足;改动面 cmd/wbot/* 4 文件 +179/-9,与 97e769b(llmreview.go)无重叠
- **待部署**:bugfix 可及时发;部署后验收:gate 失败 → 推送器打「window open, will re-check」而非 skip(30s tick 复查),重试成功卡片送达,窗口外仍 skip(无回归)

## Next

- 部署(operator/老板,与 #62 同批次或独立)
- P2-1(双 FAILED 即时 skip/首条起算)、P2-2(窗口日志降频)排期下一轮;P3(注释漂移 gate.go:47-51、FAILED+REJECTED 验收测试、5min 边界测试)排期
- 观察:775 为 15d0a9a+97e769b 部署下评估,若 gate 失败将 skip(#63 未部署前预期),作部署前后对照
