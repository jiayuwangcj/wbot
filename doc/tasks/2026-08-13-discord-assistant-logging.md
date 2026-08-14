# 2026-08-13 Discord 智能助手服务端日志(可观测性补强)

## Goal

/ask 全链路可观测:每个问题从入队到答案落地的每个阶段都有服务端日志,故障排查不再依赖用户转述。实测暴露的缺口(2026-08-13):第 2 问 PID 431 耗时偏长疑似 180s 超时,但服务端无任何记录;答案内容/超时结果/队列状态全部不可见。

## Constraints

- 只改 `cmd/wbot/discord_scheduler.go`(queueAsk/askWorker)与对应测试;不动 assistant 接口与 `discord_assistant.go` 的行为(那里已有 api_key REDACT 逻辑)。
- 错误信息可能含敏感值(claude CLI 输出):`err.Error()` 在 Ask 内部已经过 REDACT(apiKey 非空时替换为 [REDACTED]),日志直接记录 `err.Error()` 即可;**不得额外打印 claude 原始输出**。
- 问题内容为老板本人输入,可记录,但必须截断(truncateRunes 80)防日志爆炸。
- 不记录 Discord token / interaction token(只记 interaction ID)。
- 日志统一走 `s.logf`(现有设施),风格与现有 `"interaction %s: /ask ...: %v"` 一致。
- 队列满分支(queueAsk 的 default)现在无日志,补上;followup 成功无日志(失败已有),补成功确认。
- 行为不变:ack 文案、队列语义、截断规则、回复格式一律不动,只加日志。
- 保持 gofmt / go vet / 单测绿;`scripts/verify.sh` 全绿。

## Links

- `doc/tasks/2026-08-12-discord-assistant.md`(#31 主任务记录,MVP 已合入)
- `cmd/wbot/discord_scheduler.go:582-626`(queueAsk + askWorker 现状,见 State 下注释)

## State

- **status**: `done`
- **coder**: codex(gpt-5.6-luna,commit 5e255b4)
- **verify**: scripts/verify.sh 全绿
- **reviewer 评审**(2026-08-13):无条件合入,bugfix,无 P0/P1;P3 观察项:① asker==nil 分支补日志(量级小,P2 亦可)② queue_depth 语义与 answer_runes 含义加注释 ③ 既有 REDACT 对编码变换后 key 的覆盖(记录即可)
- **合入**:2026-08-13 合入 feat/llm-signal-endpoint(merge commit)

## Next(实现要点)

1. **queueAsk**:
   - 入队成功(select send 分支):打日志,含 interaction ID + 截断问题。
   - 队列满(default 分支):打日志,含 interaction ID(ack 文案保留)。
2. **askWorker**(处理一个 req):
   - 处理开始:打日志,含截断问题 + interaction ID + 队列深度 `len(s.askCh)`。
   - Ask 返回后:成功打日志(耗时 + 回答 rune 数);失败打日志(耗时 + `err.Error()`,已 REDACT)。
   - followup 发出后:成功打日志(答案已发,注明截断与否);失败保持现有日志。
   - 参考现有代码:

     ```go
     // queueAsk ack 后把问题 append 进 FIFO 队列;askWorker 单进程串行消费。
     // 老板指令(2026-08-13):① deferred 后立即回 ack,避免「正在响应」长时间
     // 悬空 ② 无论多少问题都排队,同一时刻只开一个 claude CLI,不并行。
     func (s *discordScheduler) queueAsk(ctx context.Context, in *discord.Interaction, question string) {
         if s.asker == nil {
             reply := "调用失败: 助手未配置"
             if err := s.dc.EditInteractionReply(ctx, s.appID, in.Token, reply); err != nil {
                 s.logf("interaction %s: /ask followup: %v", in.ID, err)
             }
             return
         }
         ack := "✅ 已收到问题,正在排队处理…\n「" + truncateRunes(question, 40) + "」"
         if err := s.dc.EditInteractionReply(ctx, s.appID, in.Token, ack); err != nil {
             s.logf("interaction %s: /ask ack: %v", in.ID, err)
         }
         select {
         case s.askCh <- askRequest{in: in, question: question}:
         default:
             // 队列满:不阻塞交互处理,明确告知稍后再试
             if err := s.dc.EditInteractionReply(ctx, s.appID, in.Token, "❌ 处理队列已满,请稍后再试"); err != nil {
                 s.logf("interaction %s: /ask queue-full: %v", in.ID, err)
             }
         }
     }

     // askWorker is the single consumer of askCh: one claude CLI process at a time,
     // FIFO order, Ask 自带 180s 超时。
     func (s *discordScheduler) askWorker(ctx context.Context) {
         for {
             select {
             case <-ctx.Done():
                 return
             case req := <-s.askCh:
                 answer, err := s.asker.Ask(ctx, req.question)
                 reply := answer
                 if err != nil {
                     reply = "调用失败: " + err.Error()
                 }
                 reply = truncateAssistantReply(reply)
                 if err := s.dc.EditInteractionReply(ctx, s.appID, req.in.Token, reply); err != nil {
                     s.logf("interaction %s: /ask followup: %v", req.in.ID, err)
                 }
             }
         }
     }
     ```

3. **测试**:现有测试保持全绿;日志行为不强求单测断言(脆弱),如有合适的现有测试结构(如 fake asker)顺带更新即可。
4. 提交署名:`Co-Authored-By: gpt-5.6-luna <noreply@openai.com>`(本任务由 codex 实现)。
