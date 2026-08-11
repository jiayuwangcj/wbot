# Sol 需求评估（2026-08-11）

- **来源**: 产品组 Sol（codex-cli gpt-5.6-sol，medium 思考）首轮独立评估
- **评估对象**: Wheel 实时链路切片 B–G、现有 backlog，以及 CLAUDE.md、doc/ORGS.md、2026-08-11 全部任务记录、doc/WHEEL_STRATEGY.md、doc/API.md 所定义的产品与工程契约
- **状态**: 待 owner/主会话消化（非正式任务，未派单）

## 现状评估

1. **总体完备性**: B–E 已形成「行情读取 → Wheel 决策 → 信号持久化 → LLM 审核组件 → Telegram 人工处置 → 模拟盘下单」主要构件，但闭环未完备。G 是当前主链路的阻碍性缺口（无 LLM_REVIEW 写入方，E 推送闸门不可达）；F 依赖 G。实际依赖顺序应明确为 `B → C → D → G → E 有效推送/处置 → F 总验收`。E 虽已合入，产品能力仍应视为「组件交付、端到端待 G/F 解锁」。
2. **任务记录真实性风险**: B、D ledger 仍保留评审修复/待评审表述，E 同时存在早期 queued 段和后续 delivered 记录，F/G 仍写 queued——与当前流水线不一致。F 收口时必须统一每个切片的最终提交、评审结论、能力状态与遗留项。
3. **Wheel 产品边界 P0 契约冲突**: WHEEL_STRATEGY.md / API.md 仍规定「只提醒、任何情况下不自动下单」「实时/自动执行永久 OUT_OF_SCOPE」「零交易 API 调用」；E 已实现 Telegram yes 后调用模拟盘 PlaceOrder，backlog 又计划实盘 --live-confirm。产品红线已变但无统一模式契约。F 不能只做文案同步，必须先由 owner 明确：模拟盘下单是否属于「人工确认执行」、实盘是否从永久 OUT_OF_SCOPE 调整为显式授权能力。
4. **B（报价 adapter）**: 可测性好，但真实字段路径未验证（基础快照键名可能与 fixture 假设不同）；运行输入尚未证明满足原子快照、统一 observed_at、source/ingested timestamp、lot size、市场/币种/交易时段一致性。真实采样完成前保持 DATA_BLOCKED，不得把 fake 等价为真实提醒 READY。
5. **C（实时 runner）**: 顺序执行/单标的失败不拖垮/状态同步合理；但 HasCashAvailable 恒 false 使 Put 路径长期阻塞（双向可用不成立）；外部调用缺 per-call timeout、每轮重复落 HOLD、每标的重复拉持仓——需预算或节流契约。
6. **D/G（LLM 闸门）**: D 覆盖结构化判定/防注入/fail-closed，密钥 env 方向正确；G 正确识别「有客户端无生产调用方」断点。缺口：审核总延迟预算、哪些错误可重试、退避次数、信号重试期间是否过期、重复审核幂等、产品规则版本追溯；REJECT/调用失败记录为 LLM_REVIEW 的 verdict/error 还是通用 REJECTED 需统一（避免模型判断与人工处置同语义）。
7. **E（Telegram 闭环）**: 基本交互覆盖好；但「处置完成」只到订单受理+订单号，不含订单拒绝/部分成交/成交/撤单回填——不是完整交易结果闭环；内存游标重启可能漏/重放；去重无 DB 原子闸门，双实例/并发回调仍可能重复下单。
8. **多用户与授权风险**: chat_ids 同时承担推送目的地和 callback 授权白名单；群聊 chat id 与操作者 user id 不是同一身份概念。多用户前必须拆分「可接收提醒的 chat」与「可执行处置的 actor」。
9. **密钥边界不一致**: LLM key 走 env；Telegram token 经 Admin UI 写入 ~/.wbot/wbot.conf 且文件优先、env 仅 fallback——与「密钥只走 env」冲突。0600 与不回显只是降低风险，不等价 env-only。向导应改为只显示环境配置状态。
10. **F 范围过载**: F 同时承担 CLI 行为、适配器测试、真实字段核对、容灾、LLM/Telegram E2E、文档及对账，失败定位困难；验收条件冲突——死网关通常只产生 HOLD，而 G 只审核 ALERT，同一场景不能同时稳定证明「DATA_BLOCKED 容灾」与「存在 LLM_REVIEW/Telegram dismiss」。应拆为至少两个独立场景；真实字段核对应为真实环境认证闸门，不能成为离线脚本永远完不成的步骤。
11. **backlog 判定**: 实盘下单路径是 P0 安全边界，不能直接进编码；多用户白名单与 LLM 延迟/重试是闭环上线前重要缺口；jsdom 与 CI binary smoke 属 P2，最小行为测试并入 F（动态表单只发一次请求、serve --wheel-run --telegram-run 未配置外部依赖时安全启动且 health 可诊断）。

## 新需求切片建议

1. **切片 H：执行模式契约与实盘安全闸门** | Goal: 统一 WHEEL_STRATEGY.md、API.md 与实际行为，明确「仅提醒 / 人工确认模拟盘执行 / 人工确认实盘执行」三种能力边界；契约确认前实盘 fail-closed。 | 验收: 文档与 CLI help 同一模式矩阵；默认启动及 --wheel-env real 均不能产生实盘写；实盘能力仅当显式 --live-confirm + 明确账户 + 有效未过期 signal + 最新 LLM APPROVE + 授权 actor 同时满足才进入下单调用；缺任一条件的 fake gateway 验收均断言零下单并留不含敏感信息的拒绝审计；模拟盘 yes 是否允许下单在契约中明确。 | 优先级: **P0，阻碍性**（阻碍实盘路径及 F 最终文档验收） | 依赖: E 已交付；G 完成；owner 对模拟盘/实盘产品边界决策。
2. **切片 I：可信实时报价认证与 DATA_BLOCKED 兜底** | Goal: 将 B/C 从「fixture 可解析」提升为符合 WHEEL_STRATEGY 的可认证原子快照输入；认证前稳定 DATA_BLOCKED。 | 验收: fixture 覆盖完整/缺字段/部分合约失败/跨时间戳/陈旧/倒挂/限流/断线；任一不满足原子性/新鲜度/字段边界的输入只产生 HOLD + DATA_BLOCKED + 精确 blocked_by；真实网关恢复后保存脱敏只读字段映射证据并核对 bid/ask、Greeks、OI、volume、lot size 与时间字段；真实认证未完成时能力表仍 DATA_BLOCKED；验收同时证明零交易调用。 | 优先级: **P0，阻碍性**（阻碍真实 ALERT，不阻碍服务以 HOLD 运行） | 依赖: B、C；F 死网关场景可复用，真实认证与离线 acceptance 分开记录。
3. **切片 J：LLM 审核时延、重试与可追溯决策** | Goal: 为 G 补齐有界审核策略，使每次审核可追溯到 signal、配置版本、规则版本与模型标识。 | 验收: 定义单次超时、总时间预算、仅对超时/限流/可恢复 5xx 重试、最大次数与退避；400/鉴权错误不重试；预算耗尽、信号过期、解析异常均 fail-closed 且不推送；同一 signal 不因重试产生多个有效 APPROVE；审计可查询 correlation、起止时间、延迟、attempt 数、verdict/error 分类与规则版本，不保存 prompt 敏感输入、不记录 key；fake LLM 对 APPROVE/REJECT/429→成功/超时/畸形响应逐项验收。 | 优先级: **P1，阻碍性**（阻碍 LLM 闸门宣称稳定可用，不阻碍 DATA_BLOCKED 运行） | 依赖: D、G；F 完整 fake ALERT 场景。
4. **切片 K：Telegram 身份授权与订单结果闭环** | Goal: 分离提醒目的地、授权操作者与密钥来源，把人工处置从「订单已提交」闭合到可审计终态。 | 验收: Telegram token 与其他 API key 只从 env 读取，Admin API/UI 不接受或落盘密钥值；非秘密的目标 chat 与授权 user 分开配置；回调必须同时匹配原消息 chat、signal 与授权 actor；未授权/过期/重复/跨 chat 回调均零下单并留审计；DB 原子幂等闸门保证并发/重启/双实例最多提交一次；订单受理后能记录 REJECTED/FILLED/PARTIAL/CANCELLED 实际终态或明确 pending，不能把 order id 当成交；yes/no/dismiss 及每个 actor 均可追溯。 | 优先级: **P1，阻碍性**（阻碍多用户与实盘；模拟盘单用户提醒可在显式受限状态下继续） | 依赖: E、H、J；F 的 Telegram E2E；账户订单查询能力。

## 待消化

- owner/主会话先裁决 Wheel 执行边界，把 H 作为实盘 backlog 前置条件
- manager 将 G 标记为当前主链 P0；F 拆成「死网关 DATA_BLOCKED」「完整 fake ALERT/LLM/Telegram」「真实报价认证」三组验收
- 统一更新 B–G ledger 真实状态与依赖
- jsdom 与 CI binary smoke 保留 P2 工程保障，最小行为测试并入 F
- 多用户白名单、订单终态、env-only 密钥要求由 K 统一承接，不扩展现有 chat_ids 双重语义

## 来源信息

- 执行: `codex exec -m gpt-5.6-sol -c 'model_reasoning_effort=medium'`（session 019fefcd-112c-71f1-8187-7c83d13c950a，tokens 61,199）
- 角色: `.claude/agents/product/sol.md`；组织表: doc/ORGS.md、CLAUDE.md（提交 0601084）
