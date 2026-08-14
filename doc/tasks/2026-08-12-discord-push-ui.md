# 2026-08-12 Discord 推送 UI 重排版(embed 分区 + 代码块表格 + 按钮两行)

- **id**: `2026-08-12-discord-push-ui`
- **created**: `2026-08-12`
- **parent**: #39 Discord 通道(公网链路实测打通:UA 过滤修复 + PING/PONG 保存成功 + embed 推送验证后,老板指令「按照 discord 的可用重新调整一版推送 UI」)

## Goal

Discord 推送卡片按 embed 能力重排版:一条消息多个 embed 分区(头卡/订单/期权/标的/理由)+ 等宽代码块对齐表格 + 按钮一行三个(✅下单/❌拒绝/⚠️Dismiss,老板明确 dismiss 不单独成行)。老板已确认 v3 预览样式(频道内已推 v1/v2/v3 三版对比,确认 v3 排版 + 按钮一行三枚)。

## 样式规格(老板确认稿)

**信号卡片(APPROVE,绿色 0x2ECC71,一条消息 5 个 embed):**

```
Embed 1 头卡: Author「🤖 Wheel Bot · 模拟盘」
  Title:  📌 信号 #N · SYMBOL · 方向
  Desc:   LLM 审核 ✅ APPROVE — 候选 <code> 已就绪,缺口方向一致
  Footer: 配置 vN · 信号 #N · MM-DD HH:MM
  Timestamp: 发送时刻 UTC RFC3339
Embed 2 📦 订单:代码块 3 行(候选/数量/限价)
Embed 3 📊 期权:代码块 3 行(行权+Δ / 到期+IV / bid-ask+OI);正股信号(BUY/SELL)省略本块
Embed 4 📈 标的:代码块 3 行(正股现价/库存缺口/目标-持仓)
Embed 5 🧠 LLM 理由:bullet 列表
按钮:一行三枚 [✅ 下单 style=3] [❌ 拒绝 style=4] [⚠️ Dismiss style=2](不拆行)
```

- 代码块对齐:等宽空格对齐,行 ≤ ~22 字符(手机不折行);缺省值显示「—」
- 正股信号 vs 期权信号分支沿用 alertMessage 的 isStockDirection 逻辑(正股:数量单位股、无期权块;期权:行权/到期/Δ·IV·OI/bid-ask)
- **拒绝卡片(灰 0x95A5A6)**:Title「❌ 信号 #N 被 LLM 审核拒绝」+ Description「SYMBOL · REJECT」+ 理由 bullet;复用同样式一致性
- noticeDiscord(结果通知)保持现状(标题状态 + 单行描述),不做结构改动

## 范围

1. **internal/discord/discord.go**:
   - Embed 结构加 `Author`/`Footer`(JSON: author{name}/footer{text});加 EmbedAuthor/EmbedFooter 小结构
   - **Message.Components 修正为 ActionRow 格式**:现 `[][]Button` 序列化为 `[[{...}]]`,Discord API 要求 `[{"type":1,"components":[{...}]}]`——**带按钮消息会 400,从未真实发送成功过**(tg-test 不带按钮,预览脚本已验证正确格式)。改类型或自定义 MarshalJSON,兼容现有测试调用方式
2. **cmd/wbot/discord_scheduler.go**:
   - pushSignalDiscord 重构为 5-embed 结构(头卡/订单/期权/标的/理由)+ 按钮一行三枚(与现布局一致)
   - pushRejectedDiscord 结构化(头卡 + 理由 bullet)
   - 保留 dismiss/rejected 交互逻辑与 audit 记录(行为不变)
3. **测试**:discord_test.go 补 ActionRow 序列化断言;discord_scheduler_test.go 补 pushSignalDiscord embed 数/代码块格式断言(现有测试随结构适配)
4. **验证**:verify.sh 全绿;tools/tg-test -discord 发送冒烟(可选加按钮模式)

## Constraints

- **CustomID 不变**:`wheel:<id>:yes / :no / :dismiss`(后端 handler 依赖)
- **Telegram 推送路径一字不动**(只改 Discord 侧)
- 颜色语义沿用:绿 APPROVE 0x2ECC71 / 灰 REJECTED 0x95A5A6 / 红 ALERT 0xE74C3C
- 敏感配置只进 ~/.wbot/;测试 fixture 假值
- verify.sh 全绿才提交;提交署名 Co-Authored-By: gpt-5.6-luna <noreply@openai.com>(codex 实现)

## Links

- 样式预览:v1/v2/v3 三版已推 Discord 频道(老板确认 v3 + 按钮两行)
- 前置:#39(done,Discord 通道基础);与 #37(LLM 定时运行)无文件重叠(本任务动 cmd/wbot/discord_scheduler.go + internal/discord,不碰 llm_signal.go)

## State

- **status**: `coding_done`(2026-08-12,待 reviewer 评审)
- **last step**: codex 完成 Action Row wire 修复(91d8584)与 Discord 推送 UI 重排版(4392194);期权 5 embed / 正股 4 embed、拒绝理由卡、三按钮一行及原 CustomID 均有回归测试
- **coder**: codex(gpt-5.6-luna)
- **verify**: `PATH="$(go env GOPATH)/bin:$PATH" scripts/verify.sh` 全绿(frontend build、gofmt、go test/vet/race、staticcheck、五平台交叉编译、CLI smoke、accept scripts)

## Next

- reviewer 评审(功能类型判定)→ 合入 feat/llm-signal-endpoint → 部署 serve → 真实信号推送验收新卡片 + 按钮闭环实测

## 样式定稿更新(2026-08-12 22:0x,老板确认 v6「先就用这一版」)

**v6 定稿规格(覆盖上面 v3 规格)**:

- 头卡 Embed:Author「🤖 Wheel Bot」;**Title 大字** = `🔴 模拟盘 · 📌 信号 #N · SYMBOL · 方向`;Description = `LLM 审核 ✅ APPROVE — 候选 <code> 已就绪,缺口方向一致`;Footer `配置 vN · 信号 #N · MM-DD HH:MM`;Timestamp
- 订单/期权/标的:**纯代码块裸排,无 Title 无分区标题**(等宽对齐,缺省「—」)
- 理由:bullet 裸排,无标题
- 按钮:一行三枚 [✅ 下单 style=3] [❌ 拒绝 style=4] [⚠️ Dismiss style=2]
- 拒绝卡片:头卡 Title `❌ 信号 #N 被 LLM 审核拒绝`(灰)+ Description 标·verdict + 理由 bullet
- 正股信号省略期权块(isStockDirection 语义沿用)

**实现进度**:codex 首轮按 v3 规格实现(5-embed + 分区 Title + Author 带「· 模拟盘」)自测中;首轮完成后追加一轮 v6 修改(Author 简化、Title 加 🔴 模拟盘、删分区 Title)。
