# 2026-08-13 LLM 生成端小型 Agent 框架(#37 迭代)

- **id**: `2026-08-13-llm-agent-framework`
- **parent**: #37(LLM 策略定时运行)
- **coder**: codex(gpt-5.6-luna,默认便宜模型)
- **基线**: `d081468`(价格字段系统注入版,feat/llm-signal-endpoint)

## Goal(老板指令 2026-08-13,原话:「可设计简单的接口提供给大模型，这样大模型如果有进一步查看信息的功能，类似agent」「框架设计为一个小型agent的样子」)

把 `internal/llmstrategy/client.go` 的「单次 prompt 生成决策」升级为**小型 LLM agent**:给模型提供简单只读工具接口,模型可按需**进一步查看信息**(行情/期权链/账户/持仓),循环至输出最终决策。

**行为变化**:
1. **工具集**(只读,简单接口,每工具 name + description + 参数):
   - `quote(symbol)`: 正股实时报价(现价/涨跌)
   - `option_chain(symbol, min_dte?, max_dte?, max_strikes?)`: 期权链(合约+strike+到期+bid/ask/last)
   - `option_quote(contract)`: 指定合约报价(含 delta/iv/open_interest)
   - `account(symbol)`: 账户资金与持仓(正股+期权持仓,qty/lot/delta)
   - 初始上下文仍给完整 snapshot(现有输入),工具是补充查看能力,不是替代。
2. **Agent 循环**:system 消息(角色+规则+工具描述)→ 模型输出要么 `tool_call`(JSON,执行结果作为 tool 消息回喂)要么最终决策 JSON;**轮数上限**(建议 5),超限视为 generation rejection;每轮调用超时(建议 30s)与总超时(180s 内,沿用现有 http client)。
3. **输出契约不变(收窄版)**:最终决策仍是 `symbol,direction,quantity,contract,reason,notes` JSON;**价格字段注入保留**(系统按行情注入 premium/strike/expiry 等,模型无权改写,见基线 d081468)。
4. **工具协议**:优先用 OpenAI-compatible `tools`/`tool_calls`(deepseek 系支持);若网关不支持,降级为「模型输出 JSON 指定工具名+参数,系统执行并回喂」的自定义协议(二选一,以可测为准,在代码注释里写明选择)。

## Constraints

- **只改 `internal/llmstrategy/`**(Client 重构 + 测试),不动 Runner/Submitter/llmsignal 接口——`Generator` 接口签名不变,Runner 零改动。
- 工具全部**只读**(行情/账户查询),不得暴露任何写操作/下单能力。
- 决策仍走现有确定性校验 + LLM 审核闸门(fail-closed 语义不变);agent 层输出只是让决策更充分,不绕过任何校验。
- 提示词骨架沿用现有 generationPrompt 的角色/规则(可扩展工具描述,不得弱化「期权仅卖出 PUT/CALL」「数量>0 才可操作」等硬规则)。
- 单测覆盖:① 模型先 tool_call 后出决策(工具执行器 fake)② 轮数超限 rejection ③ 工具返回错误时模型仍可出决策或正确失败 ④ 现有全部测试保持绿。
- **提示词缓存组织(2026-08-13 老板指令)**:「静态前置、动态后置、顺序固定」——system 消息只含绝对静态内容(角色/硬规则/工具定义/JSON 契约),内容与顺序固定(工具定义固定排序,不随轮次/输入变化);快照/账户/工具结果等动态内容全部放 user/tool 消息且在 system 之后,位置固定;不得把动态值拼进 system(模板插值破坏前缀缓存);agent 循环每轮新消息追加在消息列表末尾保持前缀;工具结果格式化固定(同一 JSON 契约,不换格式)。
- `scripts/verify.sh` 全绿才提交;提交署名 `Co-Authored-By: gpt-5.6-luna <noreply@openai.com>`。

## Links

- `internal/llmstrategy/runner.go`(基线:注入逻辑 + generationPrompt,agent 化只动 Client)
- `doc/tasks/2026-08-13-llm-strategy-scheduler.md`(#37 主任务,已合入)
- `doc/tasks/2026-08-12-llm-prompt-framework.md`(提示词框架:骨架固定+参数插槽,agent 化应继承该纪律)

## State

- **status**: `in_progress`
- **coder**: codex(gpt-5.6-luna),worktree `.claude/worktrees/llm-agent-framework`,分支 `feat/llm-agent-framework`,基线 d081468

## Next

1. codex 实现:Client agent 化 + 单测,verify.sh 全绿,独立提交。
2. reviewer 评审(健壮性/工具只读性/循环终止性/超时/契约)。
3. 合入 → 部署 → 实跑观察 agent 行为(工具调用是否真实发生、决策质量)。
