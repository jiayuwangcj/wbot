# 2026-08-12 订单时效性(5 分钟自动过期 + 再来一单 + 防误点一致性)

- **id**: `2026-08-12-discord-order-expiry`
- **created**: `2026-08-12`
- **parent**: 老板指令 2026-08-12(订单有时效性:可以要求再来一单,但本单 5 分钟后自动过期;误点击也误掉;防止「过期的订单提示却正常下单」)

## Goal

信号推送后确认窗口 **5 分钟**;**到点自动编辑原卡片:标注「⏰ 已过期」并把按钮全部删掉**(物理上无法误点);任何路径都不执行下单(行为与标注严格一致,fail-closed);「再来一单」不生成按钮——由智能助手完成(老板拍板:自己跟大模型说,助手触发重新评估出新信号)。

## 设计决策(老板拍板 2026-08-12:过期标注已过期+按钮全删;再来一单走助手)

- **过期基准**:信号 `created_at` + 5 分钟(信号→审核→推送通常 <1 分钟,误差可接受;后续可升级为记录推送时刻)
- **过期动作(主动,非惰性)**:推送时保存 message_id;goroutine 定时器到点(5min)执行 `PATCH` 编辑原消息——embed 标注「⏰ 已过期」+ **components 全部清空(所有按钮删除,含再来一单)**;同时执行器层校验兜底(防定时器漏跑/竞态):按钮已删,物理无法误点;执行器仍校验 fail-closed
- **编辑能力前置**:CreateMessage 需返回 message_id(现丢弃响应体);internal/discord 加 EditMessage(`PATCH /channels/{id}/messages/{mid}`)
- **再来一单(无按钮,走助手)**:用户对智能助手说「给 XX 再来一单」→ 助手功能操作 → 触发该 symbol **立即重新评估**(复用 wheel 评估逻辑)→ 有 ALERT 则推送新信号卡片(新 ID、新 5 分钟窗口);无信号则提示「当前无新信号」。**该能力归 2026-08-12-discord-assistant 任务(切片 2:功能操作路由)**,本任务只留过期机制
- **已知限制**:定时器在 serve 内存,serve 重启会丢已推送卡片的到期标注(记录为已知项,MVP 可接受)
- **并发**:重新评估与现有 wheel runner 并发由现有机制管;避免重复推:走现有去重/推送逻辑

## 范围

1. internal/discord:CreateMessage 返回 message_id;加 EditMessage(PATCH 消息,支持改 embeds/components)
2. 推送记录:pushSignalDiscord 保存 message_id(+deadline)于内存 map
3. 过期定时器:到点编辑卡片(标注 + 删按钮 + 留「🔄 再来一单」);幂等(只执行一次)
4. **fail-closed 加固**:下单执行器内部校验 `now > created_at + 5min` → 过期返回明确错误,不留「标注过期却下单」路径(单测:过期信号无论走哪条入口都 0 下单)
5. 测试:过期边界(4:59 可下/5:00 拒绝)、定时编辑幂等、竞态(过期瞬间并发点击)
6. 配置:过期窗口常量(或 wbot.conf 可配,默认 300s)
7. 「再来一单」不实现按钮——能力归 2026-08-12-discord-assistant(切片 2:功能操作路由,助手触发 symbol 重新评估)

## Constraints

- 有效窗口内按钮布局不变(一行三枚);过期编辑后仅留「🔄 再来一单」(`wheel:<id>:again`),下单/拒绝按钮删除
- 下单行为在有效窗口内零变化(不破坏现有确认闭环与 confirmMu 防重入)
- Telegram 路径不动(挂起);共享执行器若改动,需保证现有行为不变
- verify.sh 全绿;测试 fixture 假值;署名按实际编写模型

## Links

- 交互基建:#39(done,按钮闭环/confirmMu)+ 2026-08-12-discord-push-ui(排版,进行中)
- 与 2026-08-12-discord-assistant(智能助手)无重叠,可并行

## State

- **status**: `in_progress`(2026-08-12 老板指令 → 记录创建)
- **last step**: 需求确认 → 本记录

## Next

- 等 discord-push-ui 收口合入(同文件 handleInteraction 串行)→ 派 codex 实现 → verify → 评审 → 部署 → 真机实测(推送后等过期点按钮看提示;再来一单看新信号)
