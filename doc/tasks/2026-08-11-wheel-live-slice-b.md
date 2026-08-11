# Wheel 实时运行切片 B:futu 期权报价 adapter + 持仓映射

- **id**: `2026-08-11-wheel-live-slice-b`
- **created**: `2026-08-11`
- **updated**: `2026-08-11`

## Goal

为 wheel 实时运行器补齐实时数据面:① `internal/futu/option_quote.go` 新增 `OptionQuotes(ctx, symbols []string) (map[string]OptionQuoteEx, error)` 批量期权实时报价(基本行情 + Greeks:bid/ask/iv/delta/theta/volume/oi);② 新包 `internal/wheelrun/positions.go` 把 futu 持仓(sim env)映射为 `wheel.DecisionInput` 的 StockShares 与 `[]wheel.OptionPosition`(期权 code 解析 strike/expiry/type,PositionSide 定多空,Delta 由报价填充)。两者均接口化便于 fake 注入,测试用 httptest fake REST。

## Constraints

- **不碰**其他切片文件:`internal/llmreview/`(切片 D)、`internal/wheelrun/runner.go`(切片 C)、telegram(切片 E)。
- **真实网关不可用**:futu-opend-rs backend disconnected(2026-08-11 实测),所有行情端点返回 `no backend connection`。期权报价的字段路径按 futu OpenD REST `/api/option-quote` 惯例定义(owner 参数 + s2c),**用 fixture 假数据固化解析结构**,真实字段验证留待后端恢复后补(doc 双链注明)。
- 复用 `SnapshotLimit`(internal/futu/client.go)限流;批量单次调用不逐合约发。
- 遵守 self-documenting-code(注释 ≤1 行)、vibe-coding 八荣八耻。

## Links

- Driven-By: 用户指令 2026-08-11「wheel 策略先实际应用到 futu 模拟盘运行起来」
- Plan: `/home/jiayu/.claude/plans/mutable-nibbling-music.md` 切片 B
- Branch: `feat/slice-b-option-quote`(worktree `.claude/worktrees/slice-b-option-quote`)

## State

- **status**: `delivered`(评审修复已合入,2026-08-11 收口)
- **last step**: coder 完成并提交 `30b3cac`(feat/slice-b-option-quote):OptionQuotes 批量报价(Greeks 解析、canonical key、缺字段留零)+ wheelrun/positions.go(code 解析、多空映射);已合入开发基线(283363d)。评审(有条件批准,无 P0)修复已完成并合入基线 `4338def`(fix(futu,wheelrun): review findings——① P1-1 运行时诊断 requested/answered/bidask_zero warn 日志;② P2×4 去 ctx、Theta 改 *float64(option_quote.go:30)、数字开头 code 显式报错、负 Qty 校验)。主会话 2026-08-11 核对:Theta 已为 *float64 ✓;后续切片(25ea2da 等)适配无回归。

## 遗留(评审记录)

- **P1-1 字段路径核对(显式验收)**:真实捕获(client_test.go:100,2026-07-31)basic 快照键为 `cur_price/high_price/open_price/volume/name/update_time`,与 fixture 假定的 `bid_price/ask_price/last_price` 体系相悖;若真实网关对期权也返回 cur_price 体系 → Bid/Ask 恒零 → 永久 DATA_BLOCKED。核对步骤已列入切片 F ③(网关恢复后 curl dump 键名 diff,**真实环境认证闸门,非离线必过**);运行器诊断日志区分形态不匹配与休市。
- P3:未知 market 裸 code key(跳过+日志)、ParseSymbol 不支持含点 underlying(US.BRK.B,留待 US 标的支持)、subscribe 非 429 失败无重试(依赖下一轮)、测试小缺口(bad s2c JSON、重复 symbol、未请求合约)——低风险,随 F 的 fixture 核对一并补。
- 确认无问题:空批不发网络、parseQuoteTime 零值→wheel.Validate 阻断而非当新鲜、零=缺失语义与领域安全组合、OptionQuoteEx 缺 Expiry/Strike 由 code 解析更权威、Sign 约定与领域一致(长正短负)。

## Next

(收口)切片 C/G 已适配新接口并合入;P1-1 字段路径核对由切片 F ③ 承接(网关恢复后执行并记入 ledger)。
