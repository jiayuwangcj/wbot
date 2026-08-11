# telegram 提醒消息重设计(用户现场逐版确认,v20 定稿)

- **id**: `2026-08-11-telegram-alert-redesign`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

把 wheel ALERT 的 telegram 推送消息按用户现场确认的 v20 设计定稿落地:
标题=标的名+策略,订单区每行一条(含限价=last 估算价),标的当前区(正股现价/bid-ask/希腊),持仓区(正股+CALL/PUT+目标+缺口),下单原因(LLM reasons 逐条),外层实线/分区细虚线,按钮「✅ 下单 / ❌ 拒绝 / ⚠️ Dismiss」。

## Constraints

- **限价 = last 成交价**(用户明确:量化盘多,bid/ask 只作参考范围;以自己估算价格为准,不成交可放弃)。futu 层 `OptionQuoteEx.Last` 已有;`wheel.OptionQuote` 缺 `Last` 字段,runner.go 映射缺 `Last: q.Last`——需补
- bid/ask 仍展示(参考),但限价不用它
- 按钮 callback data 格式不变(`wheel:<id>:yes/no/dismiss`),仅按钮文案改
- dismiss 语义=「程序异常,今日不再提醒」(语义在代码注释,不进按钮文案)
- Telegram HTML 不支持:表格/字号/颜色/边框/背景。对齐手段 = `<code>` 等宽 + 空格填充(标签列宽 10 半角,中文=2 格);分割线:外层 `━━━` 实线,分区 `┄┄┄` 细虚线;重要数字 `<b><code>` 双强调
- 零新依赖;纯文本拼接;go vet/test 全绿;相关单测同步更新

## Links

- Driven-By: 用户现场逐版确认(v1→v20,Telegram 实测链路已通:chat_id 8490380501,msg 系列已在手机验证)
- 上游: doc/tasks/2026-08-11-flake-limiter-crossprocess.md(并行,互不重叠)
- Branch: `feat/telegram-alert-redesign`(worktree `.claude/worktrees/telegram-alert-redesign`)

## v20 定稿排版

```
<b>📌 HK.09988 · 卖出认购 (SELL CALL)</b>
━━━━━━━━━━━━━━━━━━━━
🎯 <b>订单</b>
候选      <b><code>HK.09988C24500</code></b>
行权      <b><code>245.00</code></b>
到期      <code>2026-08-28</code> (剩 17 天)
数量      <b><code>10</code></b> 张
限价      <b><code>1.28</code></b> (估算)
┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄
📊 <b>标的当前</b>
正股现价  <b><code>244.80</code></b>
bid/ask   <code>1.20</code>/<code>1.35</code>
希腊      Δ <code>0.42</code> · IV <code>0.25</code> · OI <code>3,204</code>
┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄
🧭 <b>持仓与策略参数</b>
正股持仓  <code>5,000</code> 股
CALL 持仓 <code>3</code> 张 · <code>245.00</code>
PUT 持仓  <code>0</code> 张
目标持仓  <code>5,000</code> 股
库存缺口  <b><code>-150</code></b> 股
┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄
🧠 <b>下单原因</b> · LLM 审核 <b>✅ APPROVE</b>
• IV 处于近期高位,卖方溢价充分
• 距到期 17 天,Theta 衰减有利
• 现价贴近行权价,回调风险可控
━━━━━━━━━━━━━━━━━━━━
信号 #1234 · 配置 v3 · 08-11 15:30
```

## 实现要点

1. `internal/wheel/wheel.go` `OptionQuote` 加 `Last float64 json:"last,omitempty"`(Last 成交价,限价锚)
2. `internal/wheelrun/runner.go` 映射补 `Last: q.Last`(wheelrun.QuoteInput? 视实际类型)
3. `cmd/wbot/telegram_scheduler.go`:
   - `alertMessage` 重写为 v20 排版(HTML 字符串拼接;标签列宽 10 半角;含全角字符宽度处理——中文 2 格;复用现有一致性)
   - 限价行:取候选 Quote 的 `Last`,显示 `(估算)`;Last 缺失时显示 `-`
   - `pushSignal` 补 LLM reasons:`ActionRecord.Details["reasons"]`([]any)逐条 `• ` 渲染到下单原因区(现有仅 APPROVE 判定)
   - 按钮文案:「✅ 下单」「❌ 拒绝」「⚠️ Dismiss」(callback data 不变)
4. 单测同步:`telegram_scheduler_test.go`(若存在)或新增——alertMessage 排版断言(含对齐/加粗/限价/原因行)、按钮文案断言
5. 自测:`scripts/verify.sh` 全绿;真实链路冒烟(serve 重启后日志无 error,设计稿比对)

## State

- **status**: `done`(2026-08-11 评审通过、合入 main;PR #333 已 MERGED)
- **executor**: codex(worktree `.claude/worktrees/telegram-alert-redesign`)
- **verification**: `PATH="$(go env GOPATH)/bin:$PATH" scripts/verify.sh` 全绿(`verify: ok`)
- 评审结论: 可合入,feature(合批发布);P2: telegram_scheduler.go CALL/PUT 持仓硬编码 `-` 未接线(数据已存在,排期接线);P3: parse_mode HTML 契约注释、dismiss 注释补「程序异常」

## Next

- ✅ 已合入(PR #333);重建 release 后 serve 重启生效
- 明早 9:30 港股开盘实测:真实 ALERT 推送排版验证
- 后续:持仓 CALL/PUT 接线(P2)、telegram 智能助手(对话+工程能力,任务 #31)
