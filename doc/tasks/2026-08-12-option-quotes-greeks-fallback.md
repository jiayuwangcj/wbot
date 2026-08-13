# OptionQuotes 双层数据链路增加 greeks 兜底(snapshot push 失效时不再 DATA_BLOCKED)

- **id**: `2026-08-12-option-quotes-greeks-fallback`
- **created**: `2026-08-12`
- **updated**: `2026-08-12`

## Goal(2026-08-12 盘中实测发现,用户 Goal「模拟盘所有验证必须成功」的阻碍项)

**现象**:2026-08-12 09:37 网关(futu-opend-rs 1.5.0)假死后重启,期权 snapshot 层失效:

- `/api/quote` 对期权合约(00700/00883/09988 全部链)返回「已订阅但缓存未就绪」(3-12s 后 answered=0),股票正常
- 重启网关容器(docker restart)无效;订阅后等 15s/60s 仍无 push 数据
- `option-quote`(greeks 端点,免订阅 pull)正常:0.1-0.2s 返回 price/mid/iv/delta/theta/vol/open_interest/contract_size(平值合约 price/vol/oi 有值,深实值/无人交易合约为空——真实市场状态)
- 影响:serve 每 tick `option-quotes` 失败 → 00700/00883/09988 全部 DATA_BLOCKED(option_quote_snapshot),wheel 实测无法进行;且 runner.go 的 OptionQuotes 失败路径不更新 watchlist status(00700 updated_at 停在 05:19)

**修复目标**:OptionQuotes 双层数据链路增加 **greeks 兜底**——snapshot 失败/空时不再整体 return err,改为用 option-quote 端点填充全部符号;snapshot 正常时行为不变(数据源优先级 snapshot > greeks 缓存)。

## 现状(2026-08-12 10:2x 实测)

- `internal/futu/option_quote.go:117-205` OptionQuotes:snapshot 分片循环任一失败 → return err(runner 收 err → runSymbol 提前退出 → 不更新 status);stale 集合只含「snapshot 有数据且 greeks 过期」的符号——**snapshot 全空时 stale 也为空,greeks 一个都不拉,返回空 map**
- `optionQuotePage`(quotePage)只有 cur_price/volume/update_time;`optionQuotePage` 只有 price/mid/iv/delta/theta/open_interest/contract_size(flexInt),**缺 vol 字段**
- `greeksEntry` 无 Volume 字段
- `wheel.OptionQuote.Validate`(wheel.go:326)硬性要求:Volume>0、OI>0、Bid/Ask 正、IV>0、Theta 非 nil、LotSize>0、QuoteTime 非零且不陈旧、delta 方向正确——**不得放宽**
- runner.go runSymbol:`now := time.Now()` 在 OptionChain 前取,DecisionInput.AsOf 用同一个 now;greeks-only 的 QuoteTime(请求时刻)若晚于 asOf → Validate「quote is from the future」拒绝
- SnapshotLimit = 1 req/3s(/api/quote 官方下限);greeks 单腿请求也走 SnapshotLimit(实测网关对 option-quote 无额外限速,连打 7 次均 0.1-0.2s);86 合约全拉 ≈258s,runner tick 无硬超时(可完成,后续 tick 顺延),greeksCache 10min TTL 让后续 tick 只拉过期合约
- multi_legs 批量被网关拒(「combo exceeds max option legs」,10 腿即拒)→ 只能单腿

## Constraints

- **不动** `internal/wheel/*`(Validate/Evaluate 校验一律不改)、`internal/llmreview/*`(并行任务)
- `runner.go` 仅允许最小改动:DecisionInput.AsOf 在 OptionQuotes 获取后取(fresh asOf),其余不动
- OptionQuotes 签名/语义:**成功返回 map(可能缺符号),不再因 snapshot 失败整体报错**;网络级失败(连不上网关)仍报错
- 限速沿用 SnapshotLimit(1 req/3s),不得为提速放宽(网关封杀红线,用户 09:43 指令)
- 测试:fake 网关 snapshot 失败 → OptionQuotes 返回 greeks 兜底数据(Volume 填充、QuoteTime 非零);snapshot 成功 → 行为与现状一致;verify.sh 全绿
- 与 2026-08-12-opentrade-init-timeout(连接管理,codex 已完成 d956149)并行,文件不重叠(本任务只动 internal/futu/option_quote.go、internal/wheelrun/runner.go)

## 实现要点

1. **optionQuotePage 加字段**:`Vol flexInt \`json:"vol"\``、`MarkPrice float64 \`json:"mark_price"\``(平值合约实测有值;深实值 null/0)
2. **greeksEntry 加 Volume int64**
3. **OptionQuotes 兜底逻辑**(核心):
   - snapshot 分片循环:失败/错误不再 return err,记 `os.Stderr` warning(含 [d/N] 进度),继续下一片
   - 组装:对每个请求符号,snapshot 有数据 → 现有逻辑;snapshot 缺失 → 直接进 stale 集合(不再要求「有 snapshot 才有 greeks」)
   - greeks 拉取逻辑不变(单腿 + SnapshotLimit + greeksCache TTL)
   - 保留 requested/answered/greeks_failed warning(运维可区分数据源)
4. **fetchOptionQuote**:`e.Volume = int64(leg.Vol)`;Last 兜底已有(price);Bid/Ask=mid 已有
5. **applyGreeks**:`if q.Volume == 0 && e.Volume > 0 { q.Volume = e.Volume }`(snapshot volume 优先,greeks-only 时填充);Last 兜底已有
6. **QuoteTime**:greeks-only 符号在 fetchOptionQuote 里 `quote.QuoteTime = time.Now()`(请求时刻 = 数据时刻,请求-响应实时)
7. **runner.go 最小改动**:`in := wheel.DecisionInput{... AsOf: ...}` 的 asOf 改为 OptionQuotes 返回后取 `asOf := time.Now()`(dailyOrders/chain 仍用原 now);避免 greeks-only 的 QuoteTime(请求时刻)晚于 asOf 被 Validate 当「future」拒绝
8. **自测**:`scripts/verify.sh` 全绿;option_quote_test 新用例(见 Constraints);runner_test 回归

## Links

- 上游: doc/tasks/2026-08-11-wheel-data-link.md(双层数据链路设计,已合入)、doc/FUTU.md §10(REST 契约)
- 实测依据: 2026-08-12 09:41-10:2x 盘中(网关重启后 snapshot push 未恢复;option-quote pull 正常;multi_legs 批量拒)
- Branch: `fix/option-quotes-greeks-fallback`(worktree `.claude/worktrees/option-quotes-greeks-fallback`)
- 执行者: codex gpt-5.6-luna(额度尽时退回 Claude coder)

## State

- **status**: `verified`(2026-08-12 10:2x 盘中部署,10:2x 00700 capability=READY,方案 C 生效:requested=86 answered=56 greeks_failed=0)
- 观察: 00700/00883/09988 全部 DATA_BLOCKED(option_quote_snapshot);网关重启无效;60s 延迟测试(10:22)确认 push 不恢复(全量订阅后等 60s quote 仍「缓存未就绪」)
- 执行: codex(2026-08-12 10:2x 派单,pid 3933961,默认模型 gpt-5.6-sol;注:派单未带 -m gpt-5.6-luna,已启动不打断,下次派单补参数)

## Next

- **10:2x 已达成**:verify 全绿 → 合入 main(1c9b013,含 e73d254+03acdfd 前置)→ 合并冲突修复(3fd2fba:trade_cancel.go 适配 withClient)→ 盘中部署 → 00700 HOLD capability=READY(signal=246)
- 待办:00883/09988 READY 确认;wheel 策略实测继续;reviewer 评审(与 #32 合并冲突已由主会话修复);PR 合入远端 main
- 走正式流程:reviewer 评审(bugfix)→ 合入 main → 下次发布
- 后续观察:网关 push 若恢复,snapshot 数据源自动回归(兜底不抢优先级)
