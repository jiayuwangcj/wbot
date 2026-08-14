# 2026-08-12 切片 A:策略共享管道抽象(#36)

- **id**: `2026-08-12-slice-a-shared-pipeline`
- **created**: `2026-08-12`
- **parent**: #36(策略接口统一抽象,wheel + LLM 两种策略)

## Goal

把「信号 → 审核 → 推送 → 确认 → 限价单 → 成交监控」抽成两种策略(wheel 固化策略 / LLM 策略)共享的管道。**不硬抽统一 Strategy 接口**(生成端一个被动一个主动,强行收敛会扭曲)——抽象落点 = 共享管道;两种策略都产出相同的 SignalRecord/Candidates,管道对其一视同仁。重构零行为变化。

## 范围(review-plan.md 切片 A,约 1-2 人天)

1. **LLMReviewer 接口收敛**:两处声明 → llmreview 包单一声明,签名逐字相同,机械合并
2. **recordLLMGate 抽取**:reviewAlert / reviewLLMSignal 骨架合一,rules 文本与 Signal 载荷作参数
3. **SignalRepository 归一**:wheelTelegramStore + LLMSignalStore 合并成完整读接口,store 实现不变
4. **Candidate/Quote 强类型化**:Candidates 构造/解码两点共用,去 map 往返

## Constraints

- **行为零变化**(重构纪律):不改输出、不改契约、不改推送语义;现有 60+ 测试是保护网
- verify.sh 全绿才提交;测试 fixture 用假值;敏感配置不进仓库
- 提交署名按实际编写模型(codex 署 gpt-5.6-luna)
- 与 #37(切片 B)文件重叠(llm_signal.go),串行不并行——本任务先行,完成后 #37 再启动

## Links

- 切片定义:doc/tasks/2026-08-12-review-plan.md「切片 A」小节
- 前置:#35(已完成,LLM 注入端点 + 闭环,feat/llm-signal-endpoint 已合入)
- 后置:#37 切片 B(LLM 策略定时运行)

## State

- **status**: `done`(已合入 feat/llm-signal-endpoint,merge commit 见下)
- **last step**: 2026-08-12 codex 实现 2824cd3 → reviewer 评审:**有条件合入**(feature),P1-1 notes 语义漂移(gate.go 条件让 wheel 路径不存 notes)→ coder 修 3b726ab(notes 条件改四象限语义 + 3 新测试)→ reviewer 复核通过 → 合入 feat/llm-signal-endpoint
- P3 观察项(不阻塞):candidate sparse 检测顺序、wireMode 零值陷阱、JSON 键序变化(compact: quote,direction,quantity,accepted;语义等价,需知会前端按 JSON 语义解析)、SignalRepository 宽接口维护面、单提交粒度
- 等价项已验证:full/compact 模式与旧 JSON 逐字段一致(含 omitempty/theta:null/零值 time.Time)、解码失败错误前缀未变、gate 三分支措辞逐字一致、HTTP 契约未变
>>>>>>> feat/shared-pipeline
